package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/libvirt"
)

type opsVMView struct {
	Name          string
	ID            string
	State         string
	Autostart     string
	VCPU          string
	MemoryCurrent string
	MemoryMaximum string
	CPUTime       string
}

func (s *Server) hostOpsPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}

	hostStatsCh := make(chan libvirt.HostResourceStats, 1)
	go func() {
		hostStatsCh <- s.libvirt.HostResourceStats(r.Context(), host.ID)
	}()

	domains, domainsErr := s.libvirt.ListDomains(r.Context(), host.ID)
	hostStats := <-hostStatsCh
	warnings := append([]string(nil), hostStats.Warnings...)
	page := pageData{Title: "运维工作台 · " + host.Name, Nav: "hosts", Data: map[string]any{
		"Host": host, "Domains": domains, "ResourceRows": []opsVMView{},
		"HostRows": readableStats(hostStats.NodeInfo), "ResourceSummary": resourceSummary(hostStats.CPUStats, hostStats.MemoryStats),
		"CPUStats": readableStats(hostStats.CPUStats), "MemoryStats": readableStats(hostStats.MemoryStats), "Warnings": warnings,
	}}
	if domainsErr != nil {
		s.logRequestError(r, "load operations domains", domainsErr)
		warnings = append(warnings, "虚拟机清单读取失败")
		page.Flash = "部分运维数据读取失败，请检查主机连接或查看服务日志。"
		page.FlashType = "warn"
	} else {
		stats, statsErr := s.libvirt.DomainStats(r.Context(), host.ID)
		if statsErr != nil {
			s.logRequestError(r, "load operations domain stats", statsErr)
			warnings = append(warnings, "虚拟机资源统计读取失败")
			page.Flash = "宿主机状态已加载，但虚拟机资源统计暂不可用。"
			page.FlashType = "warn"
		}
		page.Data.(map[string]any)["ResourceRows"] = buildOpsVMRows(domains, stats)
	}
	page.Data.(map[string]any)["Warnings"] = warnings
	s.render(w, r, "ops", page)
}

func buildOpsVMRows(domains []libvirt.Domain, stats []libvirt.DomainResourceStats) []opsVMView {
	byName := make(map[string]libvirt.DomainResourceStats, len(stats))
	for _, item := range stats {
		byName[item.Name] = item
	}
	rows := make([]opsVMView, 0, len(domains))
	for _, domain := range domains {
		row := opsVMView{Name: domain.Name, ID: domain.ID, State: domain.State, Autostart: domain.Autostart, VCPU: "-", MemoryCurrent: "-", MemoryMaximum: "-", CPUTime: "-"}
		if domain.VCPUs > 0 {
			row.VCPU = strconv.Itoa(domain.VCPUs)
		}
		row.MemoryCurrent = "未报告实际占用"
		row.MemoryMaximum = formatKiB(domain.MaxMemoryKiB)
		if item, ok := byName[domain.Name]; ok {
			if item.State != "" && item.State != "unknown" {
				row.State = item.State
			}
			if usedKiB, ok := guestMemoryUsedKiB(item); ok {
				row.MemoryCurrent = formatKiB(usedKiB)
			}
			if item.BalloonMaximum > 0 {
				row.MemoryMaximum = formatKiB(item.BalloonMaximum)
			}
			row.CPUTime = formatCPUTime(item.CPUTimeNanos)
		}
		if row.ID == "" {
			row.ID = "-"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func formatBytes(value uint64) string {
	if value == 0 {
		return "-"
	}
	const unit = 1024.0
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(value)
	index := 0
	for size >= unit && index < len(units)-1 {
		size /= unit
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

func formatKiB(value uint64) string {
	if value == 0 {
		return "-"
	}
	return formatBytes(value * 1024)
}

func formatCPUTime(nanos uint64) string {
	if nanos == 0 {
		return "-"
	}
	seconds := nanos / uint64(time.Second)
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

var batchVMActionLabels = map[string]string{
	"start":         "启动",
	"shutdown":      "正常关机",
	"reboot":        "重启",
	"suspend":       "挂起",
	"resume":        "恢复",
	"autostart-on":  "启用自启动",
	"autostart-off": "取消自启动",
}

func normalizeBatchVMNames(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if err := libvirt.ValidateName(name); err != nil {
			return nil, fmt.Errorf("虚拟机名称无效: %w", err)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("请至少选择一台虚拟机")
	}
	if len(names) > 50 {
		return nil, fmt.Errorf("单次批量操作最多选择 50 台虚拟机")
	}
	return names, nil
}

func batchActionCompatible(action, state string) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	switch action {
	case "start":
		return state == "shut off" || state == "crashed"
	case "shutdown":
		return state == "running" || state == "paused"
	case "reboot", "suspend":
		return state == "running"
	case "resume":
		return state == "paused"
	case "autostart-on", "autostart-off":
		return true
	default:
		return false
	}
}

func batchNeedsConfirmation(action string) bool {
	switch action {
	case "shutdown", "reboot", "suspend":
		return true
	default:
		return false
	}
}

func (s *Server) vmBatchAction(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	opsURL := "/hosts/" + host.ID + "/ops"
	action := strings.TrimSpace(r.Form.Get("action"))
	if _, ok := batchVMActionLabels[action]; !ok {
		s.redirectError(w, r, opsURL, "请选择有效的批量操作")
		return
	}
	names, err := normalizeBatchVMNames(r.Form["vm"])
	if err != nil {
		s.redirectError(w, r, opsURL, err.Error())
		return
	}
	if batchNeedsConfirmation(action) && r.Form.Get("confirm") != "yes" {
		s.redirectError(w, r, opsURL, "批量关机、重启或挂起需要显式确认")
		return
	}
	domains, err := s.libvirt.ListDomains(r.Context(), host.ID)
	if err != nil {
		s.logRequestError(r, "load batch domains", err)
		s.redirectError(w, r, opsURL, "无法读取当前虚拟机状态，请稍后重试")
		return
	}
	states := make(map[string]string, len(domains))
	for _, domain := range domains {
		states[domain.Name] = domain.State
	}
	for _, name := range names {
		state, ok := states[name]
		if !ok {
			s.redirectError(w, r, opsURL, fmt.Sprintf("虚拟机 %s 不存在或已被删除", name))
			return
		}
		if !batchActionCompatible(action, state) {
			s.redirectError(w, r, opsURL, fmt.Sprintf("虚拟机 %s 当前状态为 %s，不能执行%s", name, state, batchVMActionLabels[action]))
			return
		}
	}
	sess, _ := sessionFrom(r.Context())
	jobIDs := make([]string, 0, len(names))
	failures := 0
	for _, name := range names {
		args, _, err := actionArgs(action, name, "")
		if err != nil {
			failures++
			continue
		}
		minimumRole, err := command.JobMinimumRole("vm-action", "virsh", args)
		if err != nil || !command.RoleAllows(sess.User.Role, minimumRole) {
			failures++
			continue
		}
		label := fmt.Sprintf("批量%s: %s", batchVMActionLabels[action], name)
		jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-action", label, "virsh", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 300))
		if err != nil {
			failures++
			continue
		}
		jobIDs = append(jobIDs, jobID)
	}
	if len(jobIDs) == 0 {
		s.redirectError(w, r, opsURL, "批量任务未能提交，请检查任务配额和主机状态")
		return
	}
	if len(jobIDs) == 1 && failures == 0 {
		// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
		http.Redirect(w, r, "/jobs/"+jobIDs[0], http.StatusSeeOther)
		return
	}
	message := fmt.Sprintf("已提交 %d 个批量任务", len(jobIDs))
	kind := "ok"
	if failures > 0 {
		message += fmt.Sprintf("，%d 个未能提交", failures)
		kind = "error"
	}
	http.Redirect(w, r, flashURL("/jobs", message, kind), http.StatusSeeOther)
}

func vmResourceArgs(kind, vm, scope, rawValue, state string) ([]string, string, error) {
	if err := libvirt.ValidateName(vm); err != nil {
		return nil, "", fmt.Errorf("虚拟机名称无效")
	}
	state = strings.ToLower(strings.TrimSpace(state))
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "config"
	}
	if scope != "config" && scope != "live" {
		return nil, "", fmt.Errorf("请选择配置持久化或在线生效")
	}
	if scope == "live" && kind != "cpu-max" && state != "running" && state != "paused" {
		return nil, "", fmt.Errorf("在线资源调整要求虚拟机正在运行")
	}
	scopeFlags := []string{"--config"}
	scopeLabel := "下次启动生效"
	if scope == "live" {
		scopeFlags = []string{"--live", "--config"}
		scopeLabel = "在线并持久化"
	}
	switch kind {
	case "cpu", "cpu-max":
		if kind == "cpu-max" && state != "shut off" {
			return nil, "", fmt.Errorf("最大 vCPU 上限只能在虚拟机关机时修改")
		}
		count, err := strconv.Atoi(strings.TrimSpace(rawValue))
		if err != nil || count < 1 || count > 1024 {
			return nil, "", fmt.Errorf("vCPU 数量必须在 1-1024 之间")
		}
		if kind == "cpu-max" {
			return []string{"setvcpus", vm, strconv.Itoa(count), "--maximum", "--config"}, fmt.Sprintf("%s: 设置最大 vCPU 为 %d", vm, count), nil
		}
		args := append([]string{"setvcpus", vm, strconv.Itoa(count)}, scopeFlags...)
		return args, fmt.Sprintf("%s: %s vCPU 为 %d", vm, scopeLabel, count), nil
	case "memory":
		bytes, err := parseScaledAmount(strings.ToUpper(strings.TrimSpace(rawValue)))
		if err != nil || bytes%1024 != 0 {
			return nil, "", fmt.Errorf("内存容量格式无效，例如 4G")
		}
		kiB := bytes / 1024
		if kiB < 64*1024 || kiB > 1<<40 {
			return nil, "", fmt.Errorf("内存容量必须在 64 MiB 到 1 TiB 之间")
		}
		args := append([]string{"setmem", vm, strconv.FormatUint(kiB, 10)}, scopeFlags...)
		return args, fmt.Sprintf("%s: %s内存为 %s", vm, scopeLabel, strings.ToUpper(strings.TrimSpace(rawValue))), nil
	default:
		return nil, "", fmt.Errorf("不支持的资源调优类型")
	}
}

func (s *Server) vmResourceAction(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms", "虚拟机名称无效")
		return
	}
	info, err := s.libvirt.DomainInfo(r.Context(), host.ID, vmName)
	if err != nil {
		s.redirectError(w, r, vmRedirect(host.ID, vmName), "无法读取虚拟机当前状态")
		return
	}
	kind := strings.TrimSpace(r.Form.Get("kind"))
	var rawValue string
	if kind == "memory" {
		rawValue = r.Form.Get("memory_amount")
	} else {
		rawValue = r.Form.Get("cpu_count")
	}
	args, label, err := vmResourceArgs(kind, vmName, r.Form.Get("scope"), rawValue, info["State"])
	if err != nil {
		s.redirectError(w, r, vmRedirect(host.ID, vmName), err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	minimumRole, err := command.JobMinimumRole("vm-resource", "virsh", args)
	if err != nil || !command.RoleAllows(sess.User.Role, minimumRole) {
		s.redirectError(w, r, vmRedirect(host.ID, vmName), "当前角色无权执行资源调优")
		return
	}
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-resource", label, "virsh", args, "", boundedOperationTimeout(s.cfg.MaxTimeout, 120))
	if err != nil {
		s.redirectError(w, r, vmRedirect(host.ID, vmName), err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func readableStats(values map[string]string) []kvRow {
	labels := map[string]string{"user": "用户态 CPU", "system": "内核态 CPU", "idle": "空闲 CPU", "iowait": "IO 等待", "total": "总内存", "free": "空闲内存", "buffers": "缓冲区", "cached": "缓存"}
	rows := make([]kvRow, 0, len(values))
	for key, value := range values {
		normalized := strings.ToLower(strings.Trim(strings.TrimSpace(key), ":"))
		if strings.Contains(normalized, "cpu.time") || strings.Contains(normalized, "cpu.user") || strings.Contains(normalized, "cpu.system") {
			continue
		}
		label, ok := labels[normalized]
		if !ok {
			label = strings.TrimSuffix(key, ":")
		}
		rows = append(rows, kvRow{Key: label, Value: readableStatValue(label, value)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

func resourceSummary(cpu, memory map[string]string) []kvRow {
	cpuText := "不可用"
	if _, ok := statPercentPresent(cpu, "idle"); ok {
		idle, _ := statPercentPresent(cpu, "idle")
		usedCPU := 100 - idle
		if usedCPU < 0 {
			usedCPU = 0
		}
		if usedCPU > 100 {
			usedCPU = 100
		}
		cpuText = fmt.Sprintf("%.1f%%", usedCPU)
	}
	total := statBytes(memory, "total")
	free := statBytes(memory, "free") + statBytes(memory, "buffers") + statBytes(memory, "cached")
	used := uint64(0)
	if total > free {
		used = total - free
	}
	memoryText := "不可用"
	if total > 0 {
		memoryText = formatBytes(used) + " / " + formatBytes(total)
	}
	freeText := "不可用"
	if total > 0 || free > 0 {
		freeText = formatBytes(free)
	}
	return []kvRow{{Key: "CPU 使用率", Value: cpuText}, {Key: "内存使用", Value: memoryText}, {Key: "可用内存", Value: freeText}}
}

func readableStatValue(key, value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return value
	}
	number, err := strconv.ParseUint(strings.ReplaceAll(fields[0], ",", ""), 10, 64)
	if err != nil {
		return value
	}
	unit := ""
	if len(fields) > 1 {
		unit = fields[1]
	}
	lowered := strings.ToLower(key + " " + unit)
	switch {
	case strings.Contains(lowered, "kib"):
		return formatBytes(number * 1024)
	case strings.Contains(lowered, "mhz"):
		if number >= 1000 {
			return fmt.Sprintf("%.2f GHz", float64(number)/1000)
		}
		return value
	default:
		return value
	}
}

func firstStat(values map[string]string, key string) string {
	for _, candidate := range []string{key, key + ":"} {
		if value, ok := values[candidate]; ok {
			return value
		}
	}
	return ""
}

func statPercentPresent(values map[string]string, key string) (float64, bool) {
	raw := strings.TrimSpace(firstStat(values, key))
	if raw == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSuffix(raw, "%"), 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func statBytes(values map[string]string, key string) uint64 {
	fields := strings.Fields(firstStat(values, key))
	if len(fields) == 0 {
		return 0
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return uint64(number * 1024)
}
