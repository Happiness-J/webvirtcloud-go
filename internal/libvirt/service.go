package libvirt

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
const maxScreenshotBytes = 8 << 20

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
			domains[i] = domainFromInfo(name, info)
		}
	})
	return domains, nil
}

func domainFromInfo(name string, info map[string]string) Domain {
	domain := Domain{Name: name, ID: info["Id"], State: info["State"], Autostart: info["Autostart"]}
	domain.VCPUs, _ = strconv.Atoi(info["CPU(s)"])
	domain.MaxMemoryKiB = parseKiBValue(info["Max memory"])
	domain.UsedMemoryKiB = parseKiBValue(info["Used memory"])
	return domain
}

var ErrDomainNotFound = errors.New("virtual machine not found")

func (s *Service) DomainInfo(ctx context.Context, hostID, name string) (map[string]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"dominfo", name}, 10*time.Second)
	if err != nil {
		output := strings.TrimSpace(res.Output())
		if strings.Contains(output, "failed to get domain") {
			return nil, fmt.Errorf("%w: %s", ErrDomainNotFound, output)
		}
		return nil, fmt.Errorf("%w: %s", err, output)
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

func (s *Service) GuestAddresses(ctx context.Context, hostID, name string) ([]GuestAddress, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	agentRes, agentErr := s.run(ctx, hostID, []string{"domifaddr", name, "--source", "agent"}, 10*time.Second)
	agentAddresses := ParseGuestAddresses(agentRes.Stdout, "guest agent")
	if agentErr == nil && len(agentAddresses) > 0 {
		return agentAddresses, nil
	}
	leaseRes, leaseErr := s.run(ctx, hostID, []string{"domifaddr", name, "--source", "lease"}, 10*time.Second)
	leaseAddresses := ParseGuestAddresses(leaseRes.Stdout, "DHCP 租约")
	if leaseErr == nil && len(leaseAddresses) > 0 {
		return leaseAddresses, nil
	}
	if agentErr != nil && leaseErr != nil {
		return nil, fmt.Errorf("%w: %s", agentErr, strings.TrimSpace(agentRes.Output()+" "+leaseRes.Output()))
	}
	return nil, nil
}

func (s *Service) BlockErrors(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.run(ctx, hostID, []string{"domblkerror", name}, 10*time.Second)
	return res.Output(), err
}

func (s *Service) MemoryStats(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.run(ctx, hostID, []string{"dommemstat", name}, 10*time.Second)
	return res.Output(), err
}

func (s *Service) GuestAgentInfo(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.runWithAccess(ctx, hostID, []string{"guestinfo", name}, 10*time.Second, false)
	if err != nil {
		return res.Output(), fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return res.Output(), nil
}

func (s *Service) RecentEvents(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.runWithAccess(ctx, hostID, []string{"event", name, "--all", "--timestamp", "--timeout", "1"}, 5*time.Second, false)
	output := res.Output()
	if err != nil && strings.Contains(strings.ToLower(output), "events received: 0") {
		return output, nil
	}
	return output, err
}

func (s *Service) ListBackups(ctx context.Context, hostID, name string) ([]Backup, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	res, err := s.run(ctx, hostID, []string{"checkpoint-list", name, "--name"}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	out := make([]Backup, 0)
	for _, line := range nonEmptyLines(res.Stdout) {
		if ValidateName(line) != nil {
			continue
		}
		out = append(out, Backup{Name: line, State: "checkpoint"})
	}
	return out, nil
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
	detail.Domain = domainFromInfo(name, info)
	var blockErr, ifaceErr, xmlErr, snapshotErr, backupErr, blockErrorErr, memoryErr, eventErr, guestErr, powerErr, addressErr error
	var wg sync.WaitGroup
	wg.Add(8)
	go func() {
		defer wg.Done()
		detail.BlockDevs, blockErr = s.BlockDevices(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.Ifaces, ifaceErr = s.Interfaces(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.Addresses, addressErr = s.GuestAddresses(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.BlockErrors, blockErrorErr = s.BlockErrors(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.MemoryStats, memoryErr = s.MemoryStats(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.Events, eventErr = s.RecentEvents(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.GuestAgent, guestErr = s.GuestAgentInfo(ctx, hostID, name)
	}()
	go func() {
		defer wg.Done()
		detail.PowerRequest, powerErr = s.PowerRequest(ctx, hostID, name)
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
	detail.Backups, backupErr = s.ListBackups(ctx, hostID, name)
	detail.Warnings = detailWarnings(blockErr, ifaceErr, xmlErr, snapshotErr, backupErr, blockErrorErr, memoryErr, eventErr, guestErr, powerErr, addressErr)
	return detail, nil
}

func ParseGuestAddresses(output, source string) []GuestAddress {
	rows := ParseTable(output)
	out := make([]GuestAddress, 0, len(rows))
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		address, prefix, ok := strings.Cut(row[3], "/")
		if !ok || net.ParseIP(address) == nil {
			continue
		}
		out = append(out, GuestAddress{Interface: row[0], Protocol: row[2], Address: address, Prefix: prefix, Source: source})
	}
	return out
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

func (s *Service) PowerRequest(ctx context.Context, hostID, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	res, err := s.run(ctx, hostID, []string{"domstate", "--reason", name}, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return parsePowerRequest(res.Stdout), nil
}

func parsePowerRequest(output string) string {
	state := strings.ToLower(strings.TrimSpace(output))
	switch {
	case strings.Contains(state, "shutdown"):
		return "guest 已收到 ACPI 关机请求，正在等待操作系统完成关机"
	case strings.Contains(state, "reboot") || strings.Contains(state, "crashed"):
		return ""
	default:
		return ""
	}
}

func guestAgentUnavailable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "guest agent is not connected") || strings.Contains(message, "客户机代理未连接") || strings.Contains(message, "guest agent is not responding")
}

func detailWarnings(blockErr, ifaceErr, xmlErr, snapshotErr, backupErr, blockErrorErr, memoryErr, eventErr, guestErr, powerErr, addressErr error) []string {
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
	if backupErr != nil {
		warnings = append(warnings, "备份信息读取失败")
	}
	if blockErrorErr != nil {
		warnings = append(warnings, "块设备错误状态读取失败")
	}
	if memoryErr != nil {
		warnings = append(warnings, "guest 内存统计读取失败")
	}
	if eventErr != nil {
		warnings = append(warnings, "近期事件读取失败")
	}
	if guestErr != nil && !guestAgentUnavailable(guestErr) {
		warnings = append(warnings, "guest agent 信息读取失败")
	}
	if powerErr != nil {
		warnings = append(warnings, "电源请求状态读取失败")
	}
	if addressErr != nil {
		warnings = append(warnings, "客户机 IP 地址读取失败")
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
		out[i].Capacity = readableCapacity(info["Capacity"])
		out[i].Allocation = readableCapacity(info["Allocation"])
		out[i].Filesystem = FilesystemFromPath(out[i].Path)
	})
	return out, nil
}

func FilesystemFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".qcow2", ".qcow":
		return "QCOW2 容器"
	case ".raw", ".img":
		return "原始磁盘容器"
	case ".iso":
		return "ISO 9660 介质"
	case ".vmdk":
		return "VMDK 容器"
	case ".vdi":
		return "VDI 容器"
	default:
		if path == "" {
			return "-"
		}
		return "未知容器"
	}
}

func readableCapacity(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 {
		return value
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil {
		return value
	}
	unit := strings.ToLower(fields[1])
	multiplier := map[string]float64{"bytes": 1, "b": 1, "kib": 1024, "mib": 1024 * 1024, "gib": 1024 * 1024 * 1024, "tib": 1024 * 1024 * 1024 * 1024}
	scale, ok := multiplier[unit]
	if !ok {
		return value
	}
	bytes := number * scale
	return humanBytes(uint64(bytes))
}

func humanBytes(value uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(value)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
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

// HostResourceStats collects the three node-level measurements used by the
// operations workbench. The queries are independent and run concurrently, but
// any one of them may fail without hiding the other usable measurements.
func (s *Service) HostResourceStats(ctx context.Context, hostID string) HostResourceStats {
	stats := HostResourceStats{
		NodeInfo:    make(map[string]string),
		CPUStats:    make(map[string]string),
		MemoryStats: make(map[string]string),
	}
	type query struct {
		index   int
		args    []string
		target  *map[string]string
		label   string
		timeout time.Duration
	}
	queries := []query{
		{index: 0, args: []string{"nodeinfo"}, target: &stats.NodeInfo, label: "节点信息", timeout: 12 * time.Second},
		{index: 1, args: []string{"nodecpustats", "--percent"}, target: &stats.CPUStats, label: "节点 CPU 统计", timeout: 12 * time.Second},
		{index: 2, args: []string{"nodememstats"}, target: &stats.MemoryStats, label: "节点内存统计", timeout: 12 * time.Second},
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var warnings [3]string
	for _, item := range queries {
		wg.Add(1)
		go func(item query) {
			defer wg.Done()
			res, err := s.run(ctx, hostID, item.args, item.timeout)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings[item.index] = item.label + "读取失败"
				return
			}
			*item.target = ParseKeyValue(res.Stdout)
		}(item)
	}
	wg.Wait()
	for _, warning := range warnings {
		if warning != "" {
			stats.Warnings = append(stats.Warnings, warning)
		}
	}
	return stats
}

// DomainStats returns resource usage for all active domains in one libvirt
// query. Querying active domains avoids an argv size proportional to the guest
// count; stopped domains still appear in the surrounding VM inventory with
// their persistent state and autostart policy.
func (s *Service) DomainStats(ctx context.Context, hostID string) ([]DomainResourceStats, error) {
	args := []string{"domstats", "--state", "--cpu-total", "--balloon", "--block", "--list-active"}
	res, err := s.run(ctx, hostID, args, 20*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	return ParseDomainStats(res.Stdout), nil
}

// ParseDomainStats parses the key=value blocks emitted by virsh domstats.
// Unknown keys are ignored so newer libvirt releases and optional statistics
// can be added without breaking the resource view.
func ParseDomainStats(output string) []DomainResourceStats {
	var out []DomainResourceStats
	var current *DomainResourceStats
	values := make(map[string]string)
	flush := func() {
		if current == nil {
			return
		}
		if state, ok := values["state.state"]; ok {
			current.State = domainStateName(state)
		}
		if value, ok := values["balloon.maximum"]; ok {
			current.BalloonMaximum, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["balloon.usable"]; ok {
			current.BalloonUsable, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["balloon.available"]; ok {
			current.BalloonAvailable, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["balloon.current"]; ok {
			current.BalloonCurrent, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["balloon.unused"]; ok {
			current.BalloonUnused, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["balloon.rss"]; ok {
			current.BalloonRSS, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["cpu.time"]; ok {
			current.CPUTimeNanos, _ = strconv.ParseUint(value, 10, 64)
		}
		if value, ok := values["vcpu.current"]; ok {
			current.VCPUs, _ = strconv.Atoi(value)
		}
		for key, value := range values {
			if strings.HasPrefix(key, "block.") && strings.HasSuffix(key, ".capacity") {
				parsed, _ := strconv.ParseUint(value, 10, 64)
				current.BlockCapacity += parsed
			}
			if strings.HasPrefix(key, "block.") && strings.HasSuffix(key, ".allocation") {
				parsed, _ := strconv.ParseUint(value, 10, 64)
				current.BlockAllocation += parsed
			}
			if strings.HasPrefix(key, "block.") && strings.HasSuffix(key, ".physical") {
				parsed, _ := strconv.ParseUint(value, 10, 64)
				current.BlockPhysical += parsed
			}
			if strings.HasPrefix(key, "block.") && strings.HasSuffix(key, ".rd.bytes") {
				parsed, _ := strconv.ParseUint(value, 10, 64)
				current.BlockReadBytes += parsed
			}
			if strings.HasPrefix(key, "block.") && strings.HasSuffix(key, ".wr.bytes") {
				parsed, _ := strconv.ParseUint(value, 10, 64)
				current.BlockWriteBytes += parsed
			}
		}
		out = append(out, *current)
		values = make(map[string]string)
	}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Domain: '") && strings.HasSuffix(line, "'") {
			flush()
			name := strings.TrimSuffix(strings.TrimPrefix(line, "Domain: '"), "'")
			if ValidateName(name) != nil {
				current = nil
				continue
			}
			current = &DomainResourceStats{Name: name}
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	flush()
	return out
}

func domainStateName(code string) string {
	switch strings.TrimSpace(code) {
	case "1":
		return "running"
	case "2":
		return "blocked"
	case "3":
		return "paused"
	case "4":
		return "shutdown"
	case "5":
		return "shut off"
	case "6":
		return "crashed"
	case "7":
		return "pmsuspended"
	default:
		return "unknown"
	}
}

func parseKiBValue(value string) uint64 {
	fields := strings.Fields(value)
	if len(fields) == 0 || (len(fields) > 1 && fields[1] != "KiB") {
		return 0
	}
	parsed, _ := strconv.ParseUint(fields[0], 10, 64)
	return parsed
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

func (s *Service) Screenshot(ctx context.Context, hostID, name string) ([]byte, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "wvc-screenshot-")
	if err != nil {
		return nil, fmt.Errorf("创建截图临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("打开截图临时目录失败: %w", err)
	}
	defer root.Close()
	file, err := root.Create("screen.png")
	if err != nil {
		return nil, fmt.Errorf("创建截图临时文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	res, err := s.runWithAccess(ctx, hostID, []string{"screenshot", name, filepath.Join(dir, "screen.png")}, 15*time.Second, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Output()))
	}
	data, err := root.ReadFile("screen.png")
	if err != nil {
		return nil, fmt.Errorf("读取虚拟机截图失败: %w", err)
	}
	if !pngSignature(data) || len(data) > maxScreenshotBytes {
		return nil, fmt.Errorf("虚拟机截图无效")
	}
	return data, nil
}

func pngSignature(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
}

func ValidateName(name string) error {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 {
		return fmt.Errorf("名称长度无效")
	}
	if name == "." || name == ".." || strings.TrimSpace(name) != name || strings.HasPrefix(name, "-") || strings.ContainsAny(name, `/\\`) || strings.Contains(name, ":") {
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

// RunReadOnly executes a bounded read-only virsh command for diagnostics that
// should be shown immediately instead of being queued as a mutating job.
func (s *Service) RunReadOnly(ctx context.Context, hostID string, args []string, timeout time.Duration) (command.Result, error) {
	if len(args) == 0 {
		return command.Result{}, fmt.Errorf("诊断命令为空")
	}
	return s.run(ctx, hostID, args, timeout)
}
