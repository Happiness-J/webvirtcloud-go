package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

type dashboardData struct {
	Hosts     []store.Host
	HostCount int
	VMCount   int
	UserCount int
	Tools     []command.Tool
	Jobs      []store.Job
	Audit     []store.Audit
	Stats     store.QueueStats
	Live      []dashboardHostLive
}

type dashboardHostLive struct {
	Host    store.Host
	Domains []dashboardVMLive
	Warning string
}

type dashboardVMLive struct {
	Name            string
	State           string
	CPU             string
	Memory          string
	Disk            string
	Usage           int
	CPUTimeNanos    uint64 `json:"CPUTimeNanos,omitempty"`
	VCPUs           int    `json:"VCPUs,omitempty"`
	BlockReadBytes  uint64 `json:"BlockReadBytes,omitempty"`
	BlockWriteBytes uint64 `json:"BlockWriteBytes,omitempty"`
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	stats, err := s.db.QueueStats(r.Context())
	if err != nil {
		s.log.Warn("load dashboard queue stats", "error", err)
	}
	jobs, err := s.db.ListJobs(r.Context(), sess.User.ID, sess.User.Role == "admin", 8)
	if err != nil {
		s.log.Warn("load dashboard jobs", "error", err)
	}
	var audits []store.Audit
	if sess.User.Role == "admin" || sess.User.Role == "operator" {
		audits, err = s.db.RecentAudit(r.Context(), 6)
		if err != nil {
			s.log.Warn("load dashboard audit", "error", err)
		}
	}
	userCount := 0
	if sess.User.Role == "admin" {
		userCount, err = s.db.CountUsers(r.Context())
		if err != nil {
			s.log.Warn("count dashboard users", "error", err)
		}
	}
	vmCount := 0
	for _, group := range s.loadVMGroups(r.Context(), hosts) {
		vmCount += len(group.Domains)
	}
	live := s.dashboardLive(r.Context(), hosts)
	s.render(w, r, "dashboard", pageData{
		Title: "概览", Nav: "dashboard",
		Data: dashboardData{
			Hosts: hosts, HostCount: len(hosts), VMCount: vmCount, UserCount: userCount,
			Tools: s.runner.Tools(), Jobs: jobs, Audit: audits, Stats: stats, Live: live,
		},
	})
}

func (s *Server) dashboardLive(ctx context.Context, hosts []store.Host) []dashboardHostLive {
	out := make([]dashboardHostLive, 0, len(hosts))
	for _, host := range hosts {
		if !host.Enabled {
			continue
		}
		stats, err := s.libvirt.DomainStats(ctx, host.ID)
		item := dashboardHostLive{Host: host, Domains: dashboardVMViews(stats)}
		if err != nil {
			s.log.Warn("load dashboard domain stats", "host", host.Name, "error", err)
			item.Warning = "实时资源读取失败"
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) hostsPage(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	redacted := make(map[string]string, len(hosts))
	for _, host := range hosts {
		uri, err := libvirt.DecryptURI(s.cipher, host)
		if err != nil {
			redacted[host.ID] = "[无法解密]"
			continue
		}
		redacted[host.ID] = redactURI(uri)
	}
	s.render(w, r, "hosts", pageData{Title: "主机", Nav: "hosts", Data: map[string]any{"Hosts": hosts, "Redacted": redacted}})
}

func (s *Server) hostNewPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "host_form", pageData{Title: "添加主机", Nav: "hosts", Data: map[string]any{"Host": store.Host{Enabled: true}, "Editing": false}})
}

func (s *Server) hostCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.Form.Get("name"))
	uri := strings.TrimSpace(r.Form.Get("uri"))
	description := strings.TrimSpace(r.Form.Get("description"))
	if err := validateDisplayText(name, 128, "主机名称"); err != nil {
		s.redirectError(w, r, "/hosts/new", err.Error())
		return
	}
	if name == "" {
		s.redirectError(w, r, "/hosts/new", "主机名称不能为空")
		return
	}
	if err := validateDisplayText(description, 2000, "主机描述"); err != nil {
		s.redirectError(w, r, "/hosts/new", err.Error())
		return
	}
	if err := command.ValidateURI(uri); err != nil || uri == "" {
		s.redirectError(w, r, "/hosts/new", "libvirt URI 无效")
		return
	}
	hostID := id.New("host")
	encrypted, err := s.cipher.SealFor("host:"+hostID, uri)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	host := store.Host{
		ID: hostID, Name: name, URICiphertext: encrypted,
		Description: description, Enabled: r.Form.Get("enabled") == "1",
	}
	if err := s.db.CreateHost(r.Context(), host); err != nil {
		if store.IsUniqueViolation(err) {
			s.redirectError(w, r, "/hosts/new", "主机名称已存在")
			return
		}
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "host.create", HostID: host.ID, RemoteIP: s.clientIP(r), Detail: map[string]any{"name": name}})
	http.Redirect(w, r, flashURL("/hosts/"+host.ID, "主机已添加", "ok"), http.StatusSeeOther)
}

func (s *Server) hostDetail(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	uri, decryptErr := libvirt.DecryptURI(s.cipher, host)
	sess, _ := sessionFrom(r.Context())
	jobs, jobsErr := s.db.ListJobsByHost(r.Context(), sess.User.ID, sess.User.Role == "admin", host.ID, 10)
	if jobsErr != nil {
		s.serverError(w, r, jobsErr)
		return
	}
	redactedURI := redactURI(uri)
	if decryptErr != nil {
		redactedURI = "[无法解密]"
	}
	s.render(w, r, "host_detail", pageData{Title: host.Name, Nav: "hosts", Data: map[string]any{"Host": host, "Redacted": redactedURI, "Jobs": jobs}})
}

func (s *Server) hostEditPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	s.render(w, r, "host_form", pageData{Title: "编辑主机", Nav: "hosts", Data: map[string]any{"Host": host, "Editing": true}})
}

func (s *Server) hostUpdate(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if err := validateDisplayText(name, 128, "主机名称"); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/edit", err.Error())
		return
	}
	if name == "" {
		s.redirectError(w, r, "/hosts/"+host.ID+"/edit", "主机名称不能为空")
		return
	}
	description := strings.TrimSpace(r.Form.Get("description"))
	if err := validateDisplayText(description, 2000, "主机描述"); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/edit", err.Error())
		return
	}
	uriChanged := false
	if raw := strings.TrimSpace(r.Form.Get("uri")); raw != "" {
		if err := command.ValidateURI(raw); err != nil {
			s.redirectError(w, r, "/hosts/"+host.ID+"/edit", "libvirt URI 无效")
			return
		}
		current, decryptErr := libvirt.DecryptURI(s.cipher, host)
		if decryptErr != nil || current != raw {
			encrypted, err := s.cipher.SealFor("host:"+host.ID, raw)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			host.URICiphertext = encrypted
			uriChanged = true
		}
	}
	host.Name = name
	host.Description = description
	host.Enabled = r.Form.Get("enabled") == "1"
	if err := s.db.UpdateHost(r.Context(), host); err != nil {
		if errors.Is(err, store.ErrHostHasActiveJobs) && uriChanged {
			s.redirectError(w, r, "/hosts/"+host.ID+"/edit", "该主机仍有排队或运行中的任务，修改连接 URI 前请等待任务结束或取消任务")
			return
		}
		if store.IsUniqueViolation(err) {
			s.redirectError(w, r, "/hosts/"+host.ID+"/edit", "主机名称已存在")
			return
		}
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "host.update", HostID: host.ID, RemoteIP: s.clientIP(r), Detail: map[string]any{"name": name, "enabled": host.Enabled, "uri_changed": uriChanged}})
	http.Redirect(w, r, flashURL("/hosts/"+host.ID, "主机已更新", "ok"), http.StatusSeeOther)
}

func (s *Server) hostDelete(w http.ResponseWriter, r *http.Request) {
	if r.Form.Get("confirm") != "yes" || r.Form.Get("ack") != "yes" {
		s.redirectError(w, r, "/hosts/"+r.PathValue("id"), "删除主机需要显式确认")
		return
	}
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	if err := s.db.DeleteHost(r.Context(), host.ID); err != nil {
		if errors.Is(err, store.ErrHostHasActiveJobs) {
			s.redirectError(w, r, "/hosts/"+host.ID, "该主机仍有排队或运行中的任务，请等待任务结束后再删除")
			return
		}
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "host.delete", HostID: host.ID, RemoteIP: s.clientIP(r), Detail: map[string]any{"name": host.Name}})
	http.Redirect(w, r, flashURL("/hosts", "主机配置已删除（不会删除宿主机上的虚拟机）", "ok"), http.StatusSeeOther)
}

func (s *Server) hostTest(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "host-test", "测试 libvirt 连接", "virsh", []string{"version"}, "", 30)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func (s *Server) hostHealth(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	hypervisor := strings.TrimSpace(r.Form.Get("hypervisor"))
	if hypervisor == "" {
		hypervisor = "qemu"
	}
	args := []string{"--quiet", hypervisor}
	if _, err := command.JobMinimumRole("host-test", "virt-host-validate", args); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, "宿主机健康检查参数无效")
		return
	}
	sess, _ := sessionFrom(r.Context())
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "host-test", "检查宿主机虚拟化环境", "virt-host-validate", args, "", 60)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func (s *Server) hostVMsPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	groups := s.loadVMGroups(r.Context(), []store.Host{host})
	groups = s.filterDesktopGroups(r, groups)
	s.render(w, r, "vms", pageData{Title: "虚拟机", Nav: "hosts", Data: map[string]any{"Groups": groups}})
}

func (s *Server) allVMsPage(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	groups := s.loadVMGroups(r.Context(), hosts)
	groups = s.filterDesktopGroups(r, groups)
	s.render(w, r, "vms", pageData{Title: "虚拟机", Nav: "vms", Data: map[string]any{"Groups": groups}})
}

func (s *Server) filterDesktopGroups(r *http.Request, groups []vmGroup) []vmGroup {
	sess, ok := sessionFrom(r.Context())
	if !ok || sess.User.Role != "desktop" {
		return groups
	}
	access, err := s.db.ListVMAccess(r.Context(), sess.User.ID)
	if err != nil {
		s.logRequestError(r, "load desktop VM access", err)
		return nil
	}
	allowed := make(map[string]map[string]bool)
	for _, item := range access {
		if allowed[item.HostID] == nil {
			allowed[item.HostID] = map[string]bool{}
		}
		allowed[item.HostID][item.VMName] = true
	}
	filtered := make([]vmGroup, 0, len(groups))
	for _, group := range groups {
		names := allowed[group.Host.ID]
		if len(names) == 0 {
			continue
		}
		domains := make([]libvirt.Domain, 0, len(group.Domains))
		for _, domain := range group.Domains {
			if names[domain.Name] {
				domains = append(domains, domain)
			}
		}
		group.Domains = domains
		filtered = append(filtered, group)
	}
	return filtered
}

type vmGroup struct {
	Host    store.Host
	Domains []libvirt.Domain
	Err     string
	cause   error
}

func (s *Server) loadVMGroups(ctx context.Context, hosts []store.Host) []vmGroup {
	groups := loadVMGroups(ctx, hosts, 8, s.libvirt.ListDomains)
	for _, group := range groups {
		if group.cause != nil {
			s.log.Warn("load host VMs", "host_id", group.Host.ID, "host", group.Host.Name, "error", group.cause)
		}
	}
	return groups
}

type listDomainsFunc func(context.Context, string) ([]libvirt.Domain, error)

func loadVMGroups(ctx context.Context, hosts []store.Host, limit int, listDomains listDomainsFunc) []vmGroup {
	if limit < 1 {
		limit = 1
	}
	groups := make([]vmGroup, len(hosts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	for i, host := range hosts {
		groups[i].Host = host
		if !host.Enabled {
			groups[i].Err = "主机已禁用"
			continue
		}
		if err := ctx.Err(); err != nil {
			groups[i].Err = "请求已取消"
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			groups[i].Err = "请求已取消"
			continue
		}
		wg.Add(1)
		go func(idx int, h store.Host) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if recovered := recover(); recovered != nil {
					groups[idx].Domains = nil
					groups[idx].Err = "读取虚拟机清单时发生内部错误"
				}
			}()
			runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			domains, err := listDomains(runCtx, h.ID)
			if err != nil {
				groups[idx].Err = "无法读取虚拟机清单，请检查主机连接或查看服务日志"
				groups[idx].cause = err
				return
			}
			for j := range domains {
				domains[j].HostID = h.ID
				domains[j].HostName = h.Name
			}
			groups[idx].Domains = domains
		}(i, host)
	}
	wg.Wait()
	return groups
}

type kvRow struct{ Key, Value string }

type vmActionView struct {
	Name, Label, Confirm            string
	Destructive, AdminOnly, Primary bool
}

func (s *Server) vmPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "名称无效", err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	detail, err := s.libvirt.Detail(r.Context(), host.ID, vmName, sess.User.Role == "admin")
	if err != nil {
		s.logRequestError(r, "load VM detail", err)
		s.renderError(w, r, http.StatusBadGateway, "无法读取虚拟机", "无法从目标主机读取虚拟机信息，请检查主机连接或查看服务日志。")
		return
	}
	var consoleWarnings []string
	if consoleAvailable(detail.Domain.State) {
		if con, consoleErr := s.libvirt.Console(r.Context(), host.ID, vmName); consoleErr == nil {
			consoleWarnings = consoleSecurityWarnings(con)
		}
	}
	infoRows := make([]kvRow, 0, len(detail.Info))
	for key, value := range detail.Info {
		infoRows = append(infoRows, kvRow{Key: key, Value: readableInfoValue(key, value)})
	}
	sort.Slice(infoRows, func(i, j int) bool { return infoRows[i].Key < infoRows[j].Key })
	actions := vmActions(sess.User.Role, detail.Domain.State, detail.Info["Autostart"])
	consoleAllowed, accessErr := s.canUseConsole(r, host.ID, vmName)
	if sess.User.Role == "desktop" && !consoleAllowed && accessErr == nil {
		s.renderError(w, r, http.StatusForbidden, "无权查看虚拟机", "当前账号没有该虚拟机的桌面授权。")
		return
	}
	if accessErr != nil {
		s.serverError(w, r, accessErr)
		return
	}
	page := pageData{Title: vmName, Nav: "vms", Data: map[string]any{
		"Host": host, "VM": vmName, "Detail": detail, "InfoRows": infoRows, "Actions": actions,
		"ConsoleWarnings": consoleWarnings, "ConsoleAllowed": consoleAllowed,
		"ScreenshotAllowed": consoleAllowed && consoleAvailable(detail.Domain.State),
		"RemovableTargets":  removableDiskTargets(detail.BlockList),
		"MaxMemoryBytes":    parseKiBInfo(detail.Info["Max memory"]),
	}}
	if len(detail.Warnings) > 0 {
		page.Flash = strings.Join(detail.Warnings, "；") + "。虚拟机基本信息仍可查看，请检查主机连接和 libvirt 日志。"
		page.FlashType = "warn"
	}
	s.render(w, r, "vm_detail", page)
}

func readableInfoValue(key, value string) string {
	if !strings.Contains(strings.ToLower(key), "memory") {
		return value
	}
	fields := strings.Fields(value)
	if len(fields) == 0 || !strings.EqualFold(fields[len(fields)-1], "KiB") {
		return value
	}
	number, err := strconv.ParseUint(strings.ReplaceAll(fields[0], ",", ""), 10, 64)
	if err != nil {
		return value
	}
	return formatBytes(number * 1024)
}

func parseKiBInfo(value string) uint64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	number, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return number * 1024
}

func removableDiskTargets(devices []libvirt.BlockDevice) []libvirt.BlockDevice {
	out := make([]libvirt.BlockDevice, 0)
	for _, device := range devices {
		if device.Device == "disk" && strings.HasPrefix(strings.ToLower(device.Target), "sd") {
			out = append(out, device)
		}
	}
	return out
}

func vmActions(role, state, autostart string) []vmActionView {
	if role == "desktop" {
		switch strings.ToLower(strings.TrimSpace(state)) {
		case "shut off", "crashed":
			return []vmActionView{{Name: "start", Label: "启动", Primary: true}}
		case "running":
			return []vmActionView{{Name: "shutdown", Label: "正常关机"}}
		default:
			return nil
		}
	}
	if role != "admin" && role != "operator" {
		return nil
	}
	state = strings.ToLower(strings.TrimSpace(state))
	autostart = strings.ToLower(strings.TrimSpace(autostart))
	actions := make([]vmActionView, 0, 6)
	switch state {
	case "shut off", "crashed":
		actions = append(actions, vmActionView{Name: "start", Label: "启动", Primary: true})
	case "running":
		actions = append(actions,
			vmActionView{Name: "shutdown", Label: "正常关机"},
			vmActionView{Name: "reboot", Label: "重启", Destructive: true, Confirm: "确定重启虚拟机吗？"},
			vmActionView{Name: "destroy", Label: "强制断电", Destructive: true, Confirm: "强制断电可能导致数据损坏，确定继续吗？"},
			vmActionView{Name: "suspend", Label: "挂起", Destructive: true, Confirm: "挂起会暂停虚拟机运行，确定继续吗？"},
		)
	case "paused":
		actions = append(actions,
			vmActionView{Name: "resume", Label: "恢复运行"},
			vmActionView{Name: "destroy", Label: "强制断电", Destructive: true, Confirm: "强制断电可能导致数据损坏，确定继续吗？"},
		)
	}
	if autostart == "enable" {
		actions = append(actions, vmActionView{Name: "autostart-off", Label: "取消开机自启", Destructive: true, Confirm: "确定取消开机自启吗？"})
	} else {
		actions = append(actions, vmActionView{Name: "autostart-on", Label: "设为开机自启"})
	}
	return actions
}

func consoleAvailable(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "paused":
		return true
	default:
		return false
	}
}

func (s *Server) storagePage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	pools, err := s.libvirt.ListPools(r.Context(), host.ID)
	data := map[string]any{"Host": host, "Pools": pools, "SelectedPool": "", "PoolInfo": []kvRow{}, "Volumes": []libvirt.Volume{}, "ResizableVolumes": []libvirt.Volume{}}
	page := pageData{Title: "存储", Nav: "hosts", Data: data}
	if err != nil {
		s.logRequestError(r, "load storage pools", err)
		page.Flash, page.FlashType = "无法读取存储池，请检查主机连接或查看服务日志。", "error"
	}
	pool := r.URL.Query().Get("pool")
	if pool != "" && err == nil {
		if err := libvirt.ValidateName(pool); err != nil {
			s.renderError(w, r, http.StatusBadRequest, "存储池名称无效", err.Error())
			return
		}
		poolFound := false
		for _, available := range pools {
			if available.Name == pool {
				poolFound = true
				break
			}
		}
		if !poolFound {
			s.renderError(w, r, http.StatusNotFound, "存储池不存在", "指定的存储池不在当前主机的存储池清单中。")
			return
		}
		data["SelectedPool"] = pool
		info, infoErr := s.libvirt.PoolInfo(r.Context(), host.ID, pool)
		if infoErr != nil {
			s.logRequestError(r, "load storage pool info", infoErr)
			page.Flash, page.FlashType = "无法读取存储池信息，请查看服务日志。", "error"
		} else {
			data["PoolInfo"] = sortedKV(info)
		}
		volumes, volumeErr := s.libvirt.ListVolumes(r.Context(), host.ID, pool)
		if volumeErr != nil {
			s.logRequestError(r, "load storage volumes", volumeErr)
			page.Flash, page.FlashType = "无法读取存储卷，请查看服务日志。", "error"
		} else {
			data["Volumes"] = volumes
			resizable := make([]libvirt.Volume, 0, len(volumes))
			for _, volume := range volumes {
				name := strings.ToLower(volume.Name)
				if !strings.HasSuffix(name, ".iso") &&
					(strings.EqualFold(volume.Type, "file") || strings.EqualFold(volume.Type, "block")) {
					resizable = append(resizable, volume)
				}
			}
			data["ResizableVolumes"] = resizable
		}
	}
	s.render(w, r, "storage", page)
}

func (s *Server) networksPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	networks, err := s.libvirt.ListNetworks(r.Context(), host.ID)
	page := pageData{Title: "网络", Nav: "hosts", Data: map[string]any{"Host": host, "Networks": networks}}
	if err != nil {
		s.logRequestError(r, "load networks", err)
		page.Flash, page.FlashType = "无法读取虚拟网络，请检查主机连接或查看服务日志。", "error"
	}
	s.render(w, r, "networks", page)
}

type userRow struct {
	User store.User
	Self bool
}

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r.URL.Query().Get("page"))
	const pageSize = 50
	users, err := s.db.ListUsersPage(r.Context(), (page-1)*pageSize, pageSize+1)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	hasNext := len(users) > pageSize
	if hasNext {
		users = users[:pageSize]
	}
	sess, _ := sessionFrom(r.Context())
	rows := make([]userRow, 0, len(users))
	for _, user := range users {
		rows = append(rows, userRow{User: user, Self: user.ID == sess.User.ID})
	}
	s.render(w, r, "users", pageData{Title: "用户", Nav: "users", Data: map[string]any{"Users": rows, "Page": page, "HasNext": hasNext, "LDAP": s.ldap != nil && s.ldap.Enabled(), "Webhook": s.hooks != nil && s.hooks.Enabled()}})
}

func (s *Server) userNewPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "user_form", pageData{Title: "新建用户", Nav: "users"})
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,64}$`)

func (s *Server) userCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.Form.Get("username"))
	role := r.Form.Get("role")
	if !usernamePattern.MatchString(username) {
		s.redirectError(w, r, "/users/new", "用户名只能包含字母、数字、下划线、点和横线，长度 3-64")
		return
	}
	if !validRole(role) {
		s.redirectError(w, r, "/users/new", "角色无效")
		return
	}
	password := r.Form.Get("password")
	if err := validateNewPassword(password); err != nil {
		s.redirectError(w, r, "/users/new", err.Error())
		return
	}
	hash, err := auth.HashPasswordContext(r.Context(), password)
	if err != nil {
		s.logRequestError(r, "hash new user password", err)
		s.redirectError(w, r, "/users/new", "无法创建用户，请稍后重试")
		return
	}
	user := store.User{ID: id.New("user"), Username: username, PasswordHash: hash, Role: role}
	if err := s.db.CreateUser(r.Context(), user); err != nil {
		if store.IsUniqueViolation(err) {
			s.redirectError(w, r, "/users/new", "用户名已存在")
			return
		}
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.create", RemoteIP: s.clientIP(r), Detail: map[string]any{"username": username, "role": role}})
	http.Redirect(w, r, flashURL("/users", "用户已创建", "ok"), http.StatusSeeOther)
}

func (s *Server) userRole(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	targetID := r.PathValue("id")
	if store.IsSystemUserID(targetID) {
		s.redirectError(w, r, "/users", "系统 API 账号由服务配置管理，不能在页面修改")
		return
	}
	if targetID == sess.User.ID {
		s.redirectError(w, r, "/users", "不能修改自己的角色")
		return
	}
	role := r.Form.Get("role")
	if !validRole(role) {
		s.redirectError(w, r, "/users", "角色无效")
		return
	}
	target, err := s.db.GetUserByID(r.Context(), targetID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	if err := s.db.UpdateUserRole(r.Context(), targetID, role); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			s.redirectError(w, r, "/users", err.Error())
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.role", RemoteIP: s.clientIP(r), Detail: map[string]any{"username": target.Username, "role": role}})
	http.Redirect(w, r, flashURL("/users", "角色已更新", "ok"), http.StatusSeeOther)
}

func (s *Server) userDisabled(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	targetID := r.PathValue("id")
	if store.IsSystemUserID(targetID) {
		s.redirectError(w, r, "/users", "系统 API 账号由服务配置管理，不能在页面修改")
		return
	}
	if targetID == sess.User.ID {
		s.redirectError(w, r, "/users", "不能禁用当前账号")
		return
	}
	disabled := r.Form.Get("disabled") == "1"
	target, err := s.db.GetUserByID(r.Context(), targetID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	if err := s.db.SetUserDisabled(r.Context(), targetID, disabled); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			s.redirectError(w, r, "/users", err.Error())
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.disabled", RemoteIP: s.clientIP(r), Detail: map[string]any{"username": target.Username, "disabled": disabled}})
	http.Redirect(w, r, flashURL("/users", "账号状态已更新", "ok"), http.StatusSeeOther)
}

func (s *Server) userAccessPage(w http.ResponseWriter, r *http.Request) {
	if store.IsSystemUserID(r.PathValue("id")) {
		s.redirectError(w, r, "/users", "系统 API 账号不支持桌面授权")
		return
	}
	target, err := s.db.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	access, err := s.db.ListVMAccess(r.Context(), target.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "user_access", pageData{Title: "桌面授权 · " + target.Username, Nav: "users", Data: map[string]any{
		"Target": target, "Hosts": hosts, "Access": access,
	}})
}

func (s *Server) userAccessUpdate(w http.ResponseWriter, r *http.Request) {
	if store.IsSystemUserID(r.PathValue("id")) {
		s.redirectError(w, r, "/users", "系统 API 账号不支持桌面授权")
		return
	}
	target, err := s.db.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	if target.Role != "desktop" {
		s.redirectError(w, r, "/users/"+target.ID+"/access", "只有桌面用户可以分配虚拟机控制台")
		return
	}
	hostID := strings.TrimSpace(r.Form.Get("host_id"))
	vmName := strings.TrimSpace(r.Form.Get("vm_name"))
	if _, err := s.db.GetHost(r.Context(), hostID); err != nil {
		s.redirectError(w, r, "/users/"+target.ID+"/access", "主机不存在")
		return
	}
	if err := libvirt.ValidateName(vmName); err != nil {
		s.redirectError(w, r, "/users/"+target.ID+"/access", "虚拟机名称无效")
		return
	}
	action := r.Form.Get("action")
	switch action {
	case "grant":
		err = s.db.GrantVMAccess(r.Context(), target.ID, hostID, vmName)
	case "revoke":
		err = s.db.RevokeVMAccess(r.Context(), target.ID, hostID, vmName)
	default:
		s.redirectError(w, r, "/users/"+target.ID+"/access", "未知授权操作")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.vm_access." + action, RemoteIP: s.clientIP(r), HostID: hostID, VMName: vmName, Detail: map[string]any{"user_id": target.ID}})
	http.Redirect(w, r, flashURL("/users/"+target.ID+"/access", "桌面授权已更新", "ok"), http.StatusSeeOther)
}

func (s *Server) userPassword(w http.ResponseWriter, r *http.Request) {
	if store.IsSystemUserID(r.PathValue("id")) {
		s.redirectError(w, r, "/users", "系统 API 账号不设置登录密码")
		return
	}
	target, err := s.db.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	password := r.Form.Get("password")
	if err := validateNewPassword(password); err != nil {
		s.redirectError(w, r, "/users", err.Error())
		return
	}
	hash, err := auth.HashPasswordContext(r.Context(), password)
	if err != nil {
		s.logRequestError(r, "hash reset password", err)
		s.redirectError(w, r, "/users", "无法重置密码，请稍后重试")
		return
	}
	if err := s.db.SetPassword(r.Context(), target.ID, hash); err != nil {
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.password_reset", RemoteIP: s.clientIP(r), Detail: map[string]any{"username": target.Username}})
	if target.ID == sess.User.ID {
		s.clearSessionCookies(w)
		http.Redirect(w, r, flashURL("/login", "密码已重置，请重新登录", "ok"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, flashURL("/users", "密码已重置，目标用户的所有会话已失效", "ok"), http.StatusSeeOther)
}

func (s *Server) accountPasswordPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "account_password", pageData{Title: "修改密码", Nav: "account"})
}

func (s *Server) accountPassword(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	current := r.Form.Get("current_password")
	password := r.Form.Get("new_password")
	confirm := r.Form.Get("confirm_password")
	if len(current) > 4096 || len(password) > 4096 || len(confirm) > 4096 {
		s.redirectError(w, r, "/account/password", "密码过长")
		return
	}
	if password != confirm {
		s.redirectError(w, r, "/account/password", "两次输入的新密码不一致")
		return
	}
	if err := validateNewPassword(password); err != nil {
		s.redirectError(w, r, "/account/password", err.Error())
		return
	}
	user, err := s.db.GetUserByID(r.Context(), sess.User.ID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	keys := reauthKeys(s.clientIP(r), user.ID)
	allowed, err := s.reserveLoginAttempt(r, keys)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !allowed {
		s.renderError(w, r, http.StatusTooManyRequests, "修改失败", "密码验证次数过多，请稍后再试。")
		return
	}
	if !auth.VerifyPasswordContext(r.Context(), current, user.PasswordHash) {
		s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username,
			Action: "account.password_change_failed", RemoteIP: s.clientIP(r)})
		s.redirectError(w, r, "/account/password", "当前密码不正确")
		return
	}
	hash, err := auth.HashPasswordContext(r.Context(), password)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetPassword(r.Context(), user.ID, hash); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.completeLoginAttempt(keys)
	s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username,
		Action: "account.password_change", RemoteIP: s.clientIP(r)})
	s.clearSessionCookies(w)
	http.Redirect(w, r, flashURL("/login", "密码已修改，所有会话已失效，请重新登录", "ok"), http.StatusSeeOther)
}

func (s *Server) auditPage(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r.URL.Query().Get("page"))
	const pageSize = 100
	audits, err := s.db.RecentAuditPage(r.Context(), pageSize+1, (page-1)*pageSize)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	hasNext := len(audits) > pageSize
	if hasNext {
		audits = audits[:pageSize]
	}
	hosts, hostErr := s.db.ListHosts(r.Context())
	hostNames := map[string]string{}
	if hostErr == nil {
		for _, host := range hosts {
			hostNames[host.ID] = host.Name
		}
	}
	s.render(w, r, "audit", pageData{Title: "审计", Nav: "audit", Data: map[string]any{"Audits": audits, "HostNames": hostNames, "Page": page, "HasNext": hasNext}})
}

func (s *Server) hostLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if store.IsNotFound(err) {
		s.renderError(w, r, http.StatusNotFound, "主机不存在", "该主机配置不存在或已删除。")
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) userLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if store.IsNotFound(err) {
		s.renderError(w, r, http.StatusNotFound, "用户不存在", "该用户不存在或已删除。")
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, path, message string) {
	http.Redirect(w, r, flashURL(path, message, "error"), http.StatusSeeOther)
}

func redactURI(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "[未配置]"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "[无效 URI]"
	}
	user := ""
	if u.User != nil && u.User.Username() != "" {
		user = u.User.Username() + "@"
	}
	host := u.Host
	path := u.EscapedPath()
	if host == "" {
		return u.Scheme + "://" + path
	}
	return u.Scheme + "://" + user + host + path
}

func validateDisplayText(value string, maxRunes int, field string) error {
	if !utf8.ValidString(value) {
		return errors.New(field + "不是有效的 UTF-8 文本")
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s不能超过 %d 个字符", field, maxRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("%s包含不可见控制字符", field)
		}
	}
	return nil
}

func validateNewPassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("密码不是有效的 UTF-8 文本")
	}
	if len(password) > 4096 {
		return errors.New("密码过长")
	}
	if utf8.RuneCountInString(password) < 10 {
		return errors.New("密码至少需要 10 个字符")
	}
	return nil
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	if page > 10000 {
		return 10000
	}
	return page
}

func sortedKV(values map[string]string) []kvRow {
	rows := make([]kvRow, 0, len(values))
	for key, value := range values {
		rows = append(rows, kvRow{Key: key, Value: value})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

func auditActionName(action string) string {
	labels := map[string]string{
		"login.success": "登录成功", "login.failure": "登录失败", "logout": "退出登录",
		"host.create": "添加主机", "host.update": "修改主机", "host.delete": "删除主机配置",
		"user.create": "创建用户", "user.update": "修改用户", "user.disable": "禁用用户", "user.enable": "启用用户",
		"user." + "password": "重置登录口令", "user.role": "修改角色", "user.access": "修改桌面授权",
		"job.enqueue": "提交任务", "job.enqueue_deduped": "复用已有任务", "job.finished": "任务完成", "job.cancel": "取消任务",
		"vm.create": "创建虚拟机", "vm.action": "虚拟机操作",
	}
	if label, ok := labels[action]; ok {
		return label
	}
	return action
}

func auditDetailText(detail map[string]any) string {
	if len(detail) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(detail))
	for key := range detail {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := detail[key]
		if text, ok := value.(string); ok {
			parts = append(parts, key+"="+text)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			parts = append(parts, key+"=…")
			continue
		}
		parts = append(parts, key+"="+string(encoded))
	}
	return strings.Join(parts, ", ")
}

func validRole(role string) bool {
	return role == "admin" || role == "operator" || role == "desktop" || role == "viewer"
}

func (s *Server) vmScreenshot(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		http.Error(w, "虚拟机名称无效", http.StatusBadRequest)
		return
	}
	allowed, accessErr := s.canUseConsole(r, host.ID, vmName)
	if accessErr != nil {
		s.serverError(w, r, accessErr)
		return
	}
	if !allowed {
		http.Error(w, "当前账号没有该虚拟机的桌面授权", http.StatusForbidden)
		return
	}
	data, err := s.libvirt.Screenshot(r.Context(), host.ID, vmName)
	if err != nil {
		s.logRequestError(r, "capture VM screenshot", err)
		http.Error(w, "无法获取虚拟机截图", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) canUseConsole(r *http.Request, hostID, vmName string) (bool, error) {
	sess, _ := sessionFrom(r.Context())
	switch sess.User.Role {
	case "admin", "operator":
		return true, nil
	case "desktop":
		return s.db.UserCanAccessVM(r.Context(), sess.User.ID, hostID, vmName)
	default:
		return false, nil
	}
}

func (s *Server) canReadVM(r *http.Request, hostID, vmName string) (bool, error) {
	sess, _ := sessionFrom(r.Context())
	switch sess.User.Role {
	case "admin", "operator", "viewer":
		return true, nil
	case "desktop":
		return s.db.UserCanAccessVM(r.Context(), sess.User.ID, hostID, vmName)
	default:
		return false, nil
	}
}

func (s *Server) apiHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.logRequestError(r, "list API hosts", err)
		writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not list hosts")
		return
	}
	type item struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	out := make([]item, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, item{ID: host.ID, Name: host.Name, Enabled: host.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": out})
}

func (s *Server) apiHostVMs(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API host for VM list", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	domains, err := s.libvirt.ListDomains(r.Context(), host.ID)
	if err != nil {
		s.logRequestError(r, "list API host virtual machines", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not query libvirt on the selected host")
		return
	}
	vms := make([]apiVMSummary, 0, len(domains))
	for _, domain := range domains {
		vms = append(vms, apiVMSummaryFromDomain(domain))
	}
	writeJSON(w, http.StatusOK, map[string]any{"host_id": host.ID, "host_name": host.Name, "vms": vms})
}

func (s *Server) apiVM(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API host for VM", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	name := r.PathValue("vm")
	if err := libvirt.ValidateName(name); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_vm_name", "Virtual machine name is invalid")
		return
	}
	allowed, accessErr := s.canReadVM(r, host.ID, name)
	if accessErr != nil || !allowed {
		if accessErr != nil {
			s.logRequestError(r, "check API VM access", accessErr)
		}
		writeAPIError(w, r, http.StatusForbidden, "forbidden", "The current API identity cannot access this virtual machine")
		return
	}
	detail, err := s.libvirt.Detail(r.Context(), host.ID, name, false)
	if err != nil {
		if errors.Is(err, libvirt.ErrDomainNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "vm_not_found", "Virtual machine not found")
			return
		}
		s.logRequestError(r, "read API virtual machine", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not read the virtual machine from libvirt")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host_id": host.ID, "host_name": host.Name,
		"vm": apiVMSummaryFromDomain(detail.Domain), "addresses": apiGuestAddresses(detail.Addresses), "info": detail.Info,
	})
}

func (s *Server) apiVMSnapshots(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeAPIError(w, r, http.StatusNotFound, "host_not_found", "Host not found")
		} else {
			s.logRequestError(r, "load API host for snapshots", err)
			writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not load the host")
		}
		return
	}
	name := r.PathValue("vm")
	if err := libvirt.ValidateName(name); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_vm_name", "Virtual machine name is invalid")
		return
	}
	allowed, accessErr := s.canReadVM(r, host.ID, name)
	if accessErr != nil || !allowed {
		if accessErr != nil {
			s.logRequestError(r, "check API snapshot access", accessErr)
		}
		writeAPIError(w, r, http.StatusForbidden, "forbidden", "The current API identity cannot access this virtual machine")
		return
	}
	if _, err := s.libvirt.DomainInfo(r.Context(), host.ID, name); err != nil {
		if errors.Is(err, libvirt.ErrDomainNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "vm_not_found", "Virtual machine not found")
			return
		}
		s.logRequestError(r, "read API VM before snapshots", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not read the virtual machine state")
		return
	}
	snapshots, err := s.libvirt.ListSnapshots(r.Context(), host.ID, name)
	if err != nil {
		s.logRequestError(r, "list API VM snapshots", err)
		writeAPIError(w, r, http.StatusBadGateway, "libvirt_unavailable", "Could not list virtual machine snapshots")
		return
	}
	type item struct {
		Name      string `json:"name"`
		CreatedAt string `json:"created_at,omitempty"`
		State     string `json:"state,omitempty"`
	}
	out := make([]item, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, item{Name: snapshot.Name, CreatedAt: snapshot.CreatedAt, State: snapshot.State})
	}
	writeJSON(w, http.StatusOK, map[string]any{"host_id": host.ID, "vm": name, "snapshots": out})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func dashboardVMViews(stats []libvirt.DomainResourceStats) []dashboardVMLive {
	out := make([]dashboardVMLive, 0, len(stats))
	for _, item := range stats {
		usedKiB, ok := guestMemoryUsedKiB(item)
		usage := 0
		memory := "客户机未报告实际内存"
		if ok {
			usage = memoryUsagePercent(usedKiB, item.BalloonMaximum)
			memory = fmt.Sprintf("%d%% · 已用 %s / 总计 %s", usage, formatKiB(usedKiB), formatKiB(item.BalloonMaximum))
		}
		disk := "磁盘占用不可用"
		// qcow2 allocation can exceed virtual capacity by a small metadata
		// overhead. Capacity is still the meaningful virtual size, while
		// physical is the actual file size on the host.
		if item.BlockCapacity > 0 {
			disk = fmt.Sprintf("虚拟容量 %s", formatBytes(item.BlockCapacity))
			if item.BlockPhysical > 0 {
				disk += fmt.Sprintf(" · 文件 %s", formatBytes(item.BlockPhysical))
			}
		}
		out = append(out, dashboardVMLive{Name: item.Name, State: item.State, CPU: "累计 " + formatCPUTime(item.CPUTimeNanos), Memory: memory, Disk: disk, Usage: usage, CPUTimeNanos: item.CPUTimeNanos, VCPUs: item.VCPUs, BlockReadBytes: item.BlockReadBytes, BlockWriteBytes: item.BlockWriteBytes})
	}
	return out
}

func memoryUsagePercent(current, maximum uint64) int {
	if maximum == 0 || current == 0 {
		return 0
	}
	if current >= maximum {
		return 100
	}
	return int(float64(current) * 100 / float64(maximum))
}

func (s *Server) apiResources(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.logRequestError(r, "list API resource hosts", err)
		writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "Could not list resource data")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"generated_at": time.Now().UTC().Format(time.RFC3339), "hosts": s.dashboardLive(r.Context(), hosts)})
}

func guestMemoryUsedKiB(item libvirt.DomainResourceStats) (uint64, bool) {
	if item.BalloonAvailable > 0 && item.BalloonUnused <= item.BalloonAvailable {
		return item.BalloonAvailable - item.BalloonUnused, true
	}
	if item.BalloonRSS > 0 {
		return item.BalloonRSS, true
	}
	return 0, false
}

func (s *Server) vmCreatePage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	networks, netErr := s.libvirt.ListNetworks(r.Context(), host.ID)
	if netErr != nil {
		s.logRequestError(r, "list networks for VM create", netErr)
	}
	active := make([]string, 0)
	inactive := make([]string, 0)
	for _, network := range networks {
		if network.Name == "" {
			continue
		}
		if network.State == "active" {
			active = append(active, network.Name)
		} else {
			inactive = append(inactive, network.Name)
		}
	}
	s.render(w, r, "vm_create", pageData{Title: "创建虚拟机", Nav: "vms", Data: map[string]any{"Host": host, "Networks": active, "InactiveNetworks": inactive, "NetworkError": netErr != nil, "ISOs": listInstallISOs(s.cfg.ImageDir)}})
}

func (s *Server) vmCreate(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	vcpus := strings.TrimSpace(r.Form.Get("vcpus"))
	memory := strings.TrimSpace(r.Form.Get("memory"))
	disk := strings.TrimSpace(r.Form.Get("disk"))
	vcpuCount, cpuErr := strconv.Atoi(vcpus)
	memoryCount, memoryErr := strconv.Atoi(memory)
	diskCount, diskErr := strconv.Atoi(disk)
	if cpuErr != nil || memoryErr != nil || diskErr != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/new", "CPU、内存和磁盘容量必须是整数")
		return
	}
	network := strings.TrimSpace(r.Form.Get("network"))
	cdrom := strings.TrimSpace(r.Form.Get("cdrom"))
	cloud := cloudInitSpec{Hostname: strings.TrimSpace(r.Form.Get("hostname")), Username: strings.TrimSpace(r.Form.Get("cloud_user")), SSHKey: strings.TrimSpace(r.Form.Get("ssh_key")), Address: strings.TrimSpace(r.Form.Get("guest_address")), Gateway: strings.TrimSpace(r.Form.Get("guest_gateway")), DNS: strings.TrimSpace(r.Form.Get("guest_dns"))}
	args, err := s.vmCreateArgs(r.Context(), host.ID, name, vcpuCount, memoryCount, diskCount, network, cdrom, "generic", cloud)
	if err != nil {
		s.log.Warn("build VM create arguments", "request_id", requestIDFrom(r.Context()), "result", "rejected")
		http.Redirect(w, r, "/hosts/"+host.ID+"/vms/new", http.StatusSeeOther)
		return
	}
	sess, _ := sessionFrom(r.Context())
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-create", "创建虚拟机 "+name, "virt-install", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 1800))
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/new", err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func (s *Server) apiDocsPage(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	exampleHost := ""
	if err == nil {
		for _, host := range hosts {
			if host.Enabled {
				exampleHost = host.ID
				break
			}
		}
	}
	s.render(w, r, "api_docs", pageData{Title: "OpenAPI", Nav: "api", Data: map[string]any{
		"ReadConfigured":     s.cfg.MetricsToken != "" || s.cfg.APIToken != "" || s.cfg.APIAdminToken != "",
		"OperatorConfigured": s.cfg.APIToken != "" || s.cfg.APIAdminToken != "",
		"AdminConfigured":    s.cfg.APIAdminToken != "",
		"ExampleHostID":      exampleHost,
	}})
}

func listInstallISOs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".iso") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if validInstallISO(dir, path) {
			out = append(out, path)
		}
	}
	return out
}

func validInstallISO(dir, path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".iso") || strings.Contains(path, "..") {
		return false
	}
	root := filepath.Clean(dir)
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	info, err := os.Stat(clean) // #nosec G703 -- clean is confined to the configured image directory and a regular .iso name.
	return err == nil && info.Mode().IsRegular()
}
