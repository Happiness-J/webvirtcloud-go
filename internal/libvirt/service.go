package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

const maxConcurrentLibvirtReads = 16

type Service struct {
	store  *store.Store
	cipher *secret.Cipher
	runner *command.Runner
	slots  chan struct{}
}

func New(db *store.Store, cipher *secret.Cipher, runner *command.Runner) *Service {
	return &Service{
		store: db, cipher: cipher, runner: runner,
		slots: make(chan struct{}, maxConcurrentLibvirtReads),
	}
}

// DecryptURI centralizes the host-bound ciphertext purpose so callers do not
// accidentally accept a URI encrypted for another host.
func DecryptURI(cipher *secret.Cipher, host store.Host) (string, error) {
	if cipher == nil {
		return "", fmt.Errorf("服务端加密密钥未配置")
	}
	uri, err := cipher.OpenFor("host:"+host.ID, host.URICiphertext)
	if err != nil {
		return "", err
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return "", fmt.Errorf("主机 %s 没有配置 libvirt 连接 URI", host.Name)
	}
	if err := command.ValidateURI(uri); err != nil {
		return "", fmt.Errorf("主机 %s 的 libvirt 连接 URI 无效: %w", host.Name, err)
	}
	return uri, nil
}

func (s *Service) run(ctx context.Context, hostID string, args []string, timeout time.Duration) (command.Result, error) {
	return s.runWithAccess(ctx, hostID, args, timeout, true)
}

func (s *Service) runWithAccess(ctx context.Context, hostID string, args []string, timeout time.Duration, readOnly bool) (command.Result, error) {
	// ListDomains and ListPools may issue several virsh calls per request.
	// Cap them process-wide so many concurrent viewers cannot multiply into an
	// unbounded number of remote libvirt connections and child processes.
	if s.slots != nil {
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		case <-ctx.Done():
			return command.Result{}, ctx.Err()
		}
	}
	host, err := s.store.GetHost(ctx, hostID)
	if err != nil {
		return command.Result{}, err
	}
	request, err := hostToolRequest(s.cipher, host, "virsh", args, readOnly, timeout)
	if err != nil {
		return command.Result{}, err
	}
	return s.runner.Run(ctx, request)
}

// hostToolRequest binds a tool invocation to the execution target encoded in a
// host profile. In particular, qemu+ssh hosts must run virsh on the target
// machine instead of passing qemu+ssh://... to virsh on the panel host.
func hostToolRequest(cipher *secret.Cipher, host store.Host, tool string, args []string, readOnly bool, timeout time.Duration) (command.Request, error) {
	if !host.Enabled {
		return command.Request{}, fmt.Errorf("主机 %s 已禁用", host.Name)
	}
	uri, err := DecryptURI(cipher, host)
	if err != nil {
		return command.Request{}, err
	}
	target, err := command.ResolveTarget(uri)
	if err != nil {
		return command.Request{}, fmt.Errorf("解析主机执行目标失败: %w", err)
	}
	if err := target.Supports(tool); err != nil {
		return command.Request{}, err
	}
	return target.Request(tool, args, "", readOnly, timeout), nil
}

func (s *Service) Version(ctx context.Context, hostID string) (string, error) {
	res, err := s.run(ctx, hostID, []string{"version"}, 8*time.Second)
	if err != nil {
		return res.Output(), err
	}
	return res.Output(), nil
}

func (s *Service) NodeInfo(ctx context.Context, hostID string) (string, error) {
	res, err := s.run(ctx, hostID, []string{"nodeinfo"}, 8*time.Second)
	return res.Output(), err
}

func (s *Service) Capabilities(ctx context.Context, hostID string) (string, error) {
	res, err := s.run(ctx, hostID, []string{"capabilities"}, 15*time.Second)
	return res.Output(), err
}

func (s *Service) ListDomains(ctx context.Context, hostID string) ([]Domain, error) {
	res, err := s.run(ctx, hostID, []string{"list", "--all", "--name"}, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	names := nonEmptyLines(res.Stdout)
	validNames := make([]string, 0, len(names))
	for _, name := range names {
		if ValidateName(name) == nil {
			validNames = append(validNames, name)
		}
	}
	domains := make([]Domain, len(validNames))
	for i := range domains {
		domains[i].Name = validNames[i]
	}
	forEachIndex(ctx, len(validNames), 8, func(i int) {
		name := validNames[i]
		info, infoErr := s.DomainInfo(ctx, hostID, name)
		if infoErr == nil {
			domains[i].ID = info["Id"]
			domains[i].State = info["State"]
		}
	})
	return domains, nil
}

func (s *Service) DomainInfo(ctx context.Context, hostID, name string) (map[string]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"dominfo", name}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return ParseKeyValue(res.Stdout), nil
}

func (s *Service) DomainXML(ctx context.Context, hostID, name string) (string, error) {
	return s.domainXML(ctx, hostID, name, false)
}

// DomainXMLSecure requests libvirt security-sensitive XML fields such as VNC
// passwords. Callers must keep the result server-side unless the requesting
// user is explicitly authorized to view those secrets.
func (s *Service) DomainXMLSecure(ctx context.Context, hostID, name string) (string, error) {
	return s.domainXML(ctx, hostID, name, true)
}

func (s *Service) domainXML(ctx context.Context, hostID, name string, includeSecurity bool) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	args := []string{"dumpxml"}
	if includeSecurity {
		args = append(args, "--security-info")
	}
	args = append(args, name)
	// A read-only libvirt connection may not request secure XML, even when
	// polkit allows the service account to read domain secrets.
	res, err := s.runWithAccess(ctx, hostID, args, 10*time.Second, !includeSecurity)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return res.Stdout, nil
}

func (s *Service) BlockDevices(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.run(ctx, hostID, []string{"domblklist", name, "--details"}, 10*time.Second)
	return res.Output(), err
}

func (s *Service) Interfaces(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.run(ctx, hostID, []string{"domiflist", name}, 10*time.Second)
	return res.Output(), err
}

func (s *Service) ListSnapshots(ctx context.Context, hostID, name string) ([]Snapshot, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"snapshot-list", name}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	rows := ParseTable(res.Stdout)
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 || ValidateName(row[0]) != nil {
			continue
		}
		snapshot := Snapshot{Name: row[0]}
		if len(row) > 1 {
			snapshot.CreatedAt = row[1]
		}
		if len(row) > 2 {
			snapshot.State = row[2]
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Service) Detail(ctx context.Context, hostID, name string, includeXML bool) (DomainDetail, error) {
	if err := ValidateName(name); err != nil {
		return DomainDetail{}, err
	}
	detail := DomainDetail{Domain: Domain{Name: name}}
	info, infoErr := s.DomainInfo(ctx, hostID, name)
	if infoErr != nil {
		return detail, infoErr
	}
	detail.Info = info
	detail.Domain.State = info["State"]
	detail.Domain.ID = info["Id"]
	var blockErr, ifaceErr, xmlErr, snapshotErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		detail.BlockDevs, blockErr = s.BlockDevices(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.Ifaces, ifaceErr = s.Interfaces(ctx, hostID, name)
	}()
	if includeXML {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detail.XML, xmlErr = s.DomainXML(ctx, hostID, name)
		}()
	}
	wg.Wait()
	detail.BlockList = ParseBlockDevices(detail.BlockDevs)
	detail.Snapshots, snapshotErr = s.ListSnapshots(ctx, hostID, name)
	detail.Warnings = detailWarnings(blockErr, ifaceErr, xmlErr, snapshotErr)
	return detail, nil
}

func ParseBlockDevices(output string) []BlockDevice {
	rows := ParseTable(output)
	out := make([]BlockDevice, 0, len(rows))
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		device := BlockDevice{Type: row[0], Device: row[1], Target: row[2], Source: row[3]}
		if device.Target == "" || device.Source == "-" {
			continue
		}
		out = append(out, device)
	}
	return out
}

func (s *Service) BlockDeviceCapacity(ctx context.Context, hostID, name, target string) (uint64, error) {
	if err := ValidateName(name); err != nil {
		return 0, err
	}
	if err := ValidateName(target); err != nil {
		return 0, fmt.Errorf("磁盘目标无效: %w", err)
	}
	res, err := s.run(ctx, hostID, []string{"domblkinfo", name, target}, 10*time.Second)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	info := ParseKeyValue(res.Stdout)
	capacity, err := strconv.ParseUint(strings.TrimSpace(info["Capacity"]), 10, 64)
	if err != nil || capacity == 0 {
		return 0, fmt.Errorf("无法解析磁盘容量")
	}
	return capacity, nil
}

func detailWarnings(blockErr, ifaceErr, xmlErr, snapshotErr error) []string {
	var warnings []string
	if blockErr != nil {
		warnings = append(warnings, "块设备信息读取失败")
	}
	if ifaceErr != nil {
		warnings = append(warnings, "网络接口信息读取失败")
	}
	if xmlErr != nil {
		warnings = append(warnings, "虚拟机 XML 读取失败")
	}
	if snapshotErr != nil {
		warnings = append(warnings, "快照信息读取失败")
	}
	return warnings
}

func (s *Service) ListPools(ctx context.Context, hostID string) ([]Pool, error) {
	res, err := s.run(ctx, hostID, []string{"pool-list", "--all", "--name"}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	names := nonEmptyLines(res.Stdout)
	out := make([]Pool, 0, len(names))
	for _, poolName := range names {
		if ValidateName(poolName) != nil {
			continue
		}
		out = append(out, Pool{Name: poolName})
	}
	forEachIndex(ctx, len(out), 8, func(i int) {
		poolName := out[i].Name
		info, infoErr := s.PoolInfo(ctx, hostID, poolName)
		if infoErr == nil {
			out[i].State = info["State"]
			out[i].Autostart = info["Autostart"]
			out[i].Persistent = info["Persistent"]
			out[i].Capacity = info["Capacity"]
			out[i].Allocation = info["Allocation"]
			out[i].Available = info["Available"]
		}
	})
	return out, nil
}

func (s *Service) PoolInfo(ctx context.Context, hostID, pool string) (map[string]string, error) {
	if err := ValidateName(pool); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"pool-info", pool}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return ParseKeyValue(res.Stdout), nil
}

func (s *Service) ListVolumes(ctx context.Context, hostID, pool string) ([]Volume, error) {
	if err := ValidateName(pool); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"vol-list", pool}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	rows := ParseTable(res.Stdout)
	out := make([]Volume, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		v := Volume{Pool: pool, Name: row[0]}
		if len(row) > 1 {
			v.Path = row[1]
		}
		out = append(out, v)
	}
	forEachIndex(ctx, len(out), 8, func(i int) {
		if ValidateName(out[i].Name) != nil {
			return
		}
		infoRes, infoErr := s.run(ctx, hostID, []string{"vol-info", "--pool", pool, out[i].Name}, 10*time.Second)
		if infoErr != nil {
			return
		}
		info := ParseKeyValue(infoRes.Stdout)
		out[i].Type = info["Type"]
		out[i].Capacity = info["Capacity"]
		out[i].Allocation = info["Allocation"]
	})
	return out, nil
}

func (s *Service) ListNetworks(ctx context.Context, hostID string) ([]Network, error) {
	res, err := s.run(ctx, hostID, []string{"net-list", "--all", "--name"}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	names := nonEmptyLines(res.Stdout)
	out := make([]Network, 0, len(names))
	for _, networkName := range names {
		if ValidateName(networkName) != nil {
			continue
		}
		out = append(out, Network{Name: networkName})
	}
	forEachIndex(ctx, len(out), 8, func(i int) {
		networkName := out[i].Name
		infoRes, infoErr := s.run(ctx, hostID, []string{"net-info", networkName}, 10*time.Second)
		if infoErr == nil {
			info := ParseKeyValue(infoRes.Stdout)
			if info["Active"] == "yes" {
				out[i].State = "active"
			} else if info["Active"] == "no" {
				out[i].State = "inactive"
			}
			out[i].Autostart = info["Autostart"]
			out[i].Persistent = info["Persistent"]
		}
	})
	return out, nil
}

// forEachIndex runs at most workers callbacks at once and keeps the worker
// goroutine count bounded even when a host exposes thousands of domains or
// pools. Results are written by index, so output order remains deterministic.
func forEachIndex(ctx context.Context, count, workers int, fn func(int)) {
	if count <= 0 {
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > count {
		workers = count
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}

type domainXML struct {
	Devices struct {
		Graphics []graphicsXML `xml:"graphics"`
	} `xml:"devices"`
}

type graphicsXML struct {
	Type      string `xml:"type,attr"`
	Port      int    `xml:"port,attr"`
	Autoport  string `xml:"autoport,attr"`
	Listen    string `xml:"listen,attr"`
	Passwd    string `xml:"passwd,attr"`
	Listeners []struct {
		Address string `xml:"address,attr"`
	} `xml:"listen"`
}

func (s *Service) Console(ctx context.Context, hostID, name string) (Console, error) {
	xmlText, err := s.DomainXMLSecure(ctx, hostID, name)
	if err != nil {
		return Console{}, err
	}
	var doc domainXML
	if err := xml.Unmarshal([]byte(xmlText), &doc); err != nil {
		return Console{}, fmt.Errorf("解析虚拟机 XML 失败: %w", err)
	}
	var candidates []graphicsXML
	for _, g := range doc.Devices.Graphics {
		if g.Type == "vnc" {
			candidates = append(candidates, g)
		}
	}
	if len(candidates) == 0 {
		for _, g := range doc.Devices.Graphics {
			return Console{Type: g.Type}, fmt.Errorf("当前 Web 控制台仅支持 VNC，虚拟机提供的是 %s", g.Type)
		}
		return Console{}, fmt.Errorf("虚拟机未配置图形控制台")
	}
	g := candidates[0]
	address := g.Listen
	for _, l := range g.Listeners {
		if l.Address != "" {
			address = l.Address
			break
		}
	}
	if address == "" {
		address = "127.0.0.1"
	}
	if g.Port <= 0 {
		return Console{}, fmt.Errorf("VNC 端口尚未分配，请先启动虚拟机")
	}
	return Console{Type: "vnc", Address: address, Port: g.Port, Password: g.Passwd}, nil
}

func ValidateName(name string) error {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 {
		return fmt.Errorf("名称长度无效")
	}
	if name == "." || name == ".." || strings.TrimSpace(name) != name || strings.HasPrefix(name, "-") || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("名称包含非法字符")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("名称包含不可见控制字符")
		}
	}
	return nil
}

func nonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

var tableColumnSeparator = regexp.MustCompile(`\s{2,}`)
var tableHeaderCell = regexp.MustCompile(`\S+(?: \S+)*`)

func ParseTable(output string) [][]string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	start := 0
	header := ""
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "---") {
			start = i + 1
			for j := i - 1; j >= 0; j-- {
				if strings.TrimSpace(lines[j]) != "" {
					header = lines[j]
					break
				}
			}
			break
		}
	}
	columnStarts := tableColumnPositions(header)
	var rows [][]string
	for _, line := range lines[start:] {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var fields []string
		if len(columnStarts) > 1 {
			fields = splitFixedColumns(line, columnStarts)
		} else {
			fields = tableColumnSeparator.Split(trimmed, -1)
			for i := range fields {
				fields[i] = strings.TrimSpace(fields[i])
			}
		}
		if len(fields) < 2 {
			fields = strings.Fields(trimmed)
		}
		if len(fields) < 2 {
			continue
		}
		rows = append(rows, fields)
	}
	return rows
}

// libvirt renders tables with padding derived from the widest cell, not with
// a fixed delimiter. Using the header's column offsets preserves repeated and
// leading spaces inside names, snapshot descriptions, and storage paths.
func tableColumnPositions(header string) []int {
	matches := tableHeaderCell.FindAllStringIndex(strings.TrimRight(header, " \t"), -1)
	positions := make([]int, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(header[match[0]:match[1]]) != "" {
			positions = append(positions, match[0])
		}
	}
	return positions
}

func splitFixedColumns(line string, starts []int) []string {
	fields := make([]string, len(starts))
	for i, start := range starts {
		if start >= len(line) {
			continue
		}
		end := len(line)
		if i+1 < len(starts) && starts[i+1] <= len(line) {
			end = starts[i+1]
		}
		fields[i] = strings.TrimSpace(line[start:end])
	}
	return fields
}

func ParseKeyValue(output string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}
