package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/command"
	consolepkg "webvirtcloud-go/internal/console"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
	"webvirtcloud-go/internal/webhook"
)

func (s *Server) enqueueCommand(r *http.Request, user store.User, hostID, kind, label, tool string, args []string, stdin string, timeoutSeconds int) (string, error) {
	host, target, err := s.commandTarget(r.Context(), hostID)
	if err != nil {
		s.logRequestError(r, "resolve command target", err)
		return "", fmt.Errorf("无法读取主机连接配置，请稍后重试")
	}
	if err := target.Supports(tool); err != nil {
		return "", err
	}
	if target.LocalToolAvailable() && !s.runner.ToolAvailable(tool) {
		return "", fmt.Errorf("执行目标 %s 上未安装工具 %s", target.Label, tool)
	}
	readOnly, _ := command.Classify(tool, args)
	if err := s.runner.Validate(target.Request(tool, args, stdin, readOnly, time.Duration(timeoutSeconds)*time.Second)); err != nil {
		return "", err
	}
	sensitive, sensitiveReason := false, ""
	if kind == "command" {
		sensitive, sensitiveReason = command.Sensitive(tool, args)
	}
	if sensitive && user.Role != "admin" {
		return "", fmt.Errorf("敏感命令仅允许管理员执行")
	}
	maxSeconds := int(s.cfg.MaxTimeout.Seconds())
	if timeoutSeconds < 10 {
		timeoutSeconds = 10
	}
	if timeoutSeconds > maxSeconds {
		return "", fmt.Errorf("超时不能超过 %d 秒", maxSeconds)
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	auditLabel := truncateRunes(label, 180)
	if sensitive {
		auditLabel = tool + " [敏感参数已隐藏]"
	}
	jobID := id.New("job")
	job := store.Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: kind, Label: auditLabel,
		Tool: tool, ArgsJSON: string(argsJSON), Stdin: stdin, ReadOnly: readOnly,
		TimeoutSeconds: timeoutSeconds, Sensitive: sensitive,
		ExpectedHostURICiphertext: host.URICiphertext,
	}
	if err := s.db.EnqueueJob(r.Context(), job); err != nil {
		if errors.Is(err, store.ErrHostChanged) {
			return "", fmt.Errorf("主机连接配置在提交期间发生变化，请重新提交")
		}
		if errors.Is(err, store.ErrJobQuotaExceeded) {
			return "", err
		}
		var duplicate *store.DuplicateJobError
		if errors.As(err, &duplicate) {
			s.audit.Record(r.Context(), store.Audit{
				ActorID: user.ID, ActorName: user.Username, Action: "job.enqueue_deduped", HostID: hostID,
				RemoteIP: s.clientIP(r), Detail: map[string]any{
					"job_id": duplicate.ExistingID, "kind": kind, "tool": tool, "sensitive": sensitive,
				},
			})
			return duplicate.ExistingID, nil
		}
		s.logRequestError(r, "enqueue job", err)
		return "", fmt.Errorf("无法创建任务，请稍后重试")
	}
	s.audit.Record(r.Context(), store.Audit{
		ActorID: user.ID, ActorName: user.Username, Action: "job.enqueue", HostID: hostID,
		RemoteIP: s.clientIP(r), Detail: map[string]any{
			"job_id": jobID, "kind": kind, "tool": tool, "read_only": readOnly,
			"label": auditLabel, "sensitive": sensitive, "sensitive_reason": sensitiveReason,
		},
	})
	if s.hooks != nil && !readOnly {
		vm := ""
		if len(args) > 1 {
			vm = args[1]
		}
		s.hooks.Send(webhook.Event{Action: "job.accepted", Actor: user.Username, HostID: hostID, VM: vm, JobID: jobID, Detail: map[string]any{"kind": kind, "tool": tool, "label": auditLabel}})
	}
	return jobID, nil
}

func (s *Server) commandTarget(ctx context.Context, hostID string) (store.Host, command.Target, error) {
	if strings.TrimSpace(hostID) == "" {
		return store.Host{}, command.Target{}, fmt.Errorf("所有命令都必须绑定一台主机")
	}
	host, err := s.db.GetHost(ctx, hostID)
	if err != nil {
		return store.Host{}, command.Target{}, fmt.Errorf("读取主机连接信息失败: %w", err)
	}
	if !host.Enabled {
		return store.Host{}, command.Target{}, fmt.Errorf("主机 %s 已禁用", host.Name)
	}
	uri, err := libvirt.DecryptURI(s.cipher, host)
	if err != nil {
		return store.Host{}, command.Target{}, fmt.Errorf("读取主机连接信息失败: %w", err)
	}
	target, err := command.ResolveTarget(uri)
	if err != nil {
		return store.Host{}, command.Target{}, fmt.Errorf("解析主机执行目标失败: %w", err)
	}
	return host, target, nil
}

func actionArgs(action, vm, snapshot string) ([]string, bool, error) {
	switch action {
	case "start":
		return []string{"start", vm}, false, nil
	case "shutdown":
		return []string{"shutdown", vm}, false, nil
	case "reboot":
		return []string{"reboot", vm}, true, nil
	case "destroy":
		return []string{"destroy", vm}, true, nil
	case "suspend":
		return []string{"suspend", vm}, false, nil
	case "resume":
		return []string{"resume", vm}, false, nil
	case "autostart-on":
		return []string{"autostart", vm}, false, nil
	case "autostart-off":
		return []string{"autostart", "--disable", vm}, false, nil
	case "undefine":
		return []string{"undefine", vm}, true, nil
	case "clone":
		if err := libvirt.ValidateName(snapshot); err != nil {
			return nil, false, fmt.Errorf("克隆名称无效")
		}
		return []string{"virt-clone", "--original", vm, "--name", snapshot, "--auto-clone"}, false, nil
	case "migrate":
		if err := command.ValidateMigrationURIPublic(snapshot); err != nil {
			return nil, true, err
		}
		return []string{"migrate", vm, "--live", snapshot}, true, nil
	case "usb-attach", "usb-detach":
		if err := command.ValidateDevicePathPublic(snapshot); err != nil {
			return nil, false, err
		}
		verb := "attach-disk"
		if action == "usb-detach" {
			verb = "detach-disk"
		}
		return []string{verb, vm, snapshot}, action == "usb-detach", nil
	case "snapshot-create", "snapshot-revert", "snapshot-delete", "backup-create", "backup-restore", "backup-delete":
		if err := libvirt.ValidateName(snapshot); err != nil {
			return nil, false, fmt.Errorf("恢复点名称无效")
		}
		switch action {
		case "snapshot-create":
			return []string{"snapshot-create-as", vm, snapshot}, false, nil
		case "snapshot-revert":
			return []string{"snapshot-revert", vm, snapshot}, true, nil
		case "snapshot-delete":
			return []string{"snapshot-delete", vm, snapshot}, true, nil
		case "backup-create":
			return []string{"backup-create-as", vm, snapshot}, false, nil
		case "backup-restore":
			return []string{"backup-restore", vm, snapshot}, true, nil
		default:
			return []string{"backup-delete", vm, snapshot}, true, nil
		}
	default:
		return nil, false, fmt.Errorf("未知操作")
	}
}

func (s *Server) vmAction(w http.ResponseWriter, r *http.Request) {
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
	sess, _ := sessionFrom(r.Context())
	action := r.Form.Get("action")
	if sess.User.Role == "desktop" {
		if action != "start" && action != "shutdown" {
			s.redirectError(w, r, vmRedirect(host.ID, vmName), "桌面用户只能启动或正常关闭已授权虚拟机")
			return
		}
		allowed, accessErr := s.canUseConsole(r, host.ID, vmName)
		if accessErr != nil || !allowed {
			s.redirectError(w, r, "/vms", "当前账号没有该虚拟机的桌面授权")
			return
		}
	}
	args, destructive, err := actionArgs(action, vmName, strings.TrimSpace(r.Form.Get("snapshot")))
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/"+url.PathEscape(vmName), err.Error())
		return
	}
	minimumRole, err := command.JobMinimumRole("vm-action", "virsh", args)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/"+url.PathEscape(vmName), err.Error())
		return
	}
	if !command.RoleAllows(sess.User.Role, minimumRole) {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/"+url.PathEscape(vmName), "当前角色无权执行该操作")
		return
	}
	if destructive && r.Form.Get("confirm") != "yes" {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/"+url.PathEscape(vmName), "该操作需要显式确认")
		return
	}
	label := fmt.Sprintf("%s: %s", vmName, action)
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "vm-action", label, "virsh", args, "", 300)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/vms/"+url.PathEscape(vmName), err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func (s *Server) commandPage(w http.ResponseWriter, r *http.Request) {
	host, target, err := s.commandTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			s.renderError(w, r, http.StatusNotFound, "主机不存在", "该主机配置不存在或已被删除。")
			return
		}
		s.logRequestError(r, "load command host", err)
		s.renderError(w, r, http.StatusBadRequest, "无法加载主机", "无法读取主机连接配置，请检查主机设置或查看服务日志。")
		return
	}
	discovery := s.commandDiscovery(r.Context(), target)
	tools := s.toolsForTarget(target, discovery)
	capabilities := make([]command.ToolCapability, 0, len(tools))
	for _, tool := range tools {
		capability := discovery.Tools[tool.Name]
		capability.Name = tool.Name
		if !tool.Available && capability.Error == "" {
			capability.Error = "执行目标未安装"
		}
		if err := target.Supports(tool.Name); err != nil && capability.Error == "" {
			capability.Error = err.Error()
		}
		capabilities = append(capabilities, capability)
	}
	s.render(w, r, "command_center", pageData{Title: "命令中心", Nav: "hosts", Data: map[string]any{
		"Host": host, "Target": commandTargetViewFor(target), "Tools": tools, "Capabilities": capabilities,
		"Total": len(command.Catalog()), "MaxTimeoutSeconds": int(s.cfg.MaxTimeout.Seconds()),
	}})
}

func (s *Server) commandCatalogAPI(w http.ResponseWriter, r *http.Request) {
	_, target, err := s.commandTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeJSONError(w, http.StatusNotFound, "host not found")
		} else {
			s.logRequestError(r, "load command catalog host", err)
			writeJSONError(w, http.StatusBadRequest, "host configuration unavailable")
		}
		return
	}
	discovery := s.commandDiscovery(r.Context(), target)
	items := command.ApplyDiscovery(command.Catalog(), discovery)
	for i := range items {
		item := &items[i]
		if err := target.Supports(item.Tool); err != nil {
			item.ToolAvailable = false
			item.ProbeState = "unsupported"
			item.UnavailableReason = err.Error()
			continue
		}
		if item.ToolAvailable {
			continue
		}
		switch target.Kind {
		case command.TargetSSH:
			item.UnavailableReason = "目标主机 " + target.Label + " 的 PATH 中未安装 " + item.Tool + "。"
		default:
			item.UnavailableReason = "执行目标 " + target.Label + " 上未安装 " + item.Tool + "。"
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items":       items,
		"discovery":   discovery,
		"target":      commandTargetViewFor(target),
		"catalogSize": len(items),
	})
}

type commandTargetView struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Remote      bool   `json:"remote"`
}

func commandTargetViewFor(target command.Target) commandTargetView {
	view := commandTargetView{Kind: string(target.Kind), Label: target.Label, Remote: target.SSH != nil}
	switch target.Kind {
	case command.TargetSSH:
		view.Description = "所有命令通过加固 OpenSSH 在目标 KVM 物理机执行；libvirt 工具使用目标机上的 qemu:/// URI，不会嵌套 SSH。"
	case command.TargetLibvirt:
		view.Description = "libvirt 感知工具由面板本机通过 --connect 管理远程 libvirt；独立 QEMU 工具需要 qemu+ssh 主机配置。"
	default:
		view.Description = "命令在运行面板服务的本机执行。"
	}
	return view
}

func (s *Server) commandDiscovery(ctx context.Context, target command.Target) command.Discovery {
	key := commandDiscoveryKey(target)
	s.catalogMu.Lock()
	if s.catalogCache == nil {
		s.catalogCache = make(map[string]*commandDiscoveryEntry)
	}
	entry := s.catalogCache[key]
	if entry != nil && !entry.cachedAt.IsZero() && time.Since(entry.cachedAt) < 5*time.Minute {
		cached := entry.discovery
		s.catalogMu.Unlock()
		return cached
	}
	if entry != nil && entry.inFlight != nil {
		wait := entry.inFlight
		stale := entry.discovery
		s.catalogMu.Unlock()
		select {
		case <-wait:
			s.catalogMu.Lock()
			result := s.catalogCache[key].discovery
			s.catalogMu.Unlock()
			return result
		case <-ctx.Done():
			if stale.Tools != nil {
				return stale
			}
			return command.Discovery{Tools: make(map[string]command.ToolCapability)}
		}
	}
	if entry == nil {
		entry = &commandDiscoveryEntry{}
		s.catalogCache[key] = entry
	}
	wait := make(chan struct{})
	entry.inFlight = wait
	s.catalogMu.Unlock()

	discovery, cacheable := s.probeCommandDiscovery(target)

	s.catalogMu.Lock()
	if cacheable {
		entry.discovery = discovery
		entry.cachedAt = time.Now()
	}
	result := discovery
	if !cacheable && entry.discovery.Tools != nil {
		result = entry.discovery
	}
	entry.inFlight = nil
	close(wait)
	s.catalogMu.Unlock()
	return result
}

func (s *Server) probeCommandDiscovery(target command.Target) (discovery command.Discovery, cacheable bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			discovery = command.Discovery{Tools: make(map[string]command.ToolCapability)}
			cacheable = false
			if s.log != nil {
				s.log.Error("command discovery panicked", "target", target.Label,
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			}
		}
	}()

	probeCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if target.SSH != nil {
		var err error
		discovery, err = command.DiscoverRemote(probeCtx, s.runner, *target.SSH, 4*time.Second)
		return discovery, err == nil
	}
	discovery = command.Discover(probeCtx, s.runner)
	return discovery, probeCtx.Err() == nil && s.discoveryCacheable(discovery)
}

func commandDiscoveryKey(target command.Target) string {
	if target.SSH == nil {
		return string(target.Kind) + "|" + target.ConnectURI
	}
	return "ssh|" + target.SSH.Destination() + "|" + strconv.Itoa(target.SSH.Port) + "|" +
		target.SSH.KeyFile + "|" + target.SSH.KnownHosts + "|" + target.ConnectURI
}

func (s *Server) toolsForTarget(target command.Target, discovery command.Discovery) []command.Tool {
	tools := s.runner.Tools()
	for i := range tools {
		tool := &tools[i]
		if target.SSH != nil {
			tool.Available = discovery.Tools[tool.Name].Available
			tool.Path = "目标主机 PATH"
		}
		if err := target.Supports(tool.Name); err != nil {
			tool.Available = false
		}
	}
	return tools
}

func (s *Server) discoveryCacheable(discovery command.Discovery) bool {
	available := make(map[string]bool, len(s.runner.Tools()))
	for _, tool := range s.runner.Tools() {
		available[tool.Name] = tool.Available
	}
	for name, capability := range discovery.Tools {
		if available[name] && capability.Error != "" {
			return false
		}
	}
	return true
}

func (s *Server) commandSubmit(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	if r.Form.Get("confirm") != "yes" {
		s.redirectError(w, r, "/hosts/"+host.ID+"/command", "提交命令前必须确认影响范围")
		return
	}
	tool := r.Form.Get("tool")
	args, err := commandArgsFromForm(r)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/command", err.Error())
		return
	}
	timeout, _ := strconv.Atoi(r.Form.Get("timeout"))
	if timeout == 0 {
		timeout = 900
	}
	sess, _ := sessionFrom(r.Context())
	label := truncateRunes(command.CommandLine(tool, args), 180)
	jobID, err := s.enqueueCommand(r, sess.User, host.ID, "command", label, tool, args, r.Form.Get("stdin"), timeout)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID+"/command", err.Error())
		return
	}
	// #nosec G710 -- jobID is generated by the enqueue path and the destination is a local fixed prefix.
	http.Redirect(w, r, "/jobs/"+jobID, http.StatusSeeOther)
}

func commandArgsFromForm(r *http.Request) ([]string, error) {
	if r.Form.Get("mode") == "advanced" {
		return parseRequiredCommandArgs(r.Form.Get("raw_args"))
	}
	raw := strings.TrimSpace(r.Form.Get("args_json"))
	if raw == "" {
		return parseRequiredCommandArgs(r.Form.Get("args"))
	}
	if len(raw) > 128*1024 {
		return nil, fmt.Errorf("结构化参数过大")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("结构化参数格式无效")
	}
	return args, nil
}

func parseRequiredCommandArgs(raw string) ([]string, error) {
	args, err := command.ParseArgs(raw)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("命令参数不能为空")
	}
	return args, nil
}

func (s *Server) jobsPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	page := parsePage(r.URL.Query().Get("page"))
	const pageSize = 100
	jobs, err := s.db.ListJobsPage(r.Context(), sess.User.ID, sess.User.Role == "admin", pageSize+1, (page-1)*pageSize)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	hasNext := len(jobs) > pageSize
	if hasNext {
		jobs = jobs[:pageSize]
	}
	stats, err := s.db.QueueStats(r.Context())
	if err != nil {
		s.log.Warn("load job queue stats", "error", err)
	}
	s.render(w, r, "jobs", pageData{Title: "任务", Nav: "jobs", Data: map[string]any{
		"Jobs": jobs, "Stats": stats, "Page": page, "HasNext": hasNext,
	}})
}

func (s *Server) jobPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	job, err := s.db.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		s.jobLookupError(w, r, err)
		return
	}
	if !s.canAccessJob(sess.User, job) {
		s.renderError(w, r, http.StatusForbidden, "无权访问", "该任务属于其他用户。")
		return
	}
	job.Sensitive = effectiveJobSensitive(job)
	protected := job.Sensitive && sess.User.Role != "admin"
	if protected {
		job.Label = job.Tool + " [敏感参数已隐藏]"
		job.ArgsJSON = `["敏感参数已隐藏"]`
		job.Stdin = ""
		job.Output = "敏感命令输出仅管理员可见。"
		job.Error = ""
	}
	var args []string
	_ = json.Unmarshal([]byte(job.ArgsJSON), &args)
	commandLine := command.CommandLine(job.Tool, args)
	if protected {
		commandLine = job.Tool + " [敏感参数已隐藏]"
	}
	s.render(w, r, "job_detail", pageData{Title: job.Label, Nav: "jobs", Data: map[string]any{
		"Job": job, "CommandLine": commandLine,
	}})
}

func (s *Server) jobCancel(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	job, err := s.db.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		s.jobLookupError(w, r, err)
		return
	}
	if !s.canAccessJob(sess.User, job) {
		s.renderError(w, r, http.StatusForbidden, "无权访问", "该任务属于其他用户。")
		return
	}
	if job.Status != "queued" && job.Status != "running" {
		s.redirectError(w, r, "/jobs/"+job.ID, "任务已经结束，无法取消")
		return
	}
	active, err := s.db.RequestCancelJob(r.Context(), job.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !active {
		s.redirectError(w, r, "/jobs/"+job.ID, "任务已经结束，无法取消")
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "job.cancel", HostID: job.HostID, RemoteIP: s.clientIP(r), Detail: map[string]any{"job_id": job.ID}})
	http.Redirect(w, r, flashURL("/jobs/"+job.ID, "取消请求已提交", "ok"), http.StatusSeeOther)
}

type jobAPIResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Output   string `json:"output"`
	Error    string `json:"error"`
	Created  string `json:"created_at,omitempty"`
	Started  string `json:"started_at,omitempty"`
	Finished string `json:"finished_at,omitempty"`
}

func (s *Server) jobAPI(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	job, err := s.db.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		if store.IsNotFound(err) {
			writeJSONError(w, http.StatusNotFound, "job not found")
		} else {
			s.log.Error("load job API", "job_id", r.PathValue("id"), "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	if !s.canAccessJob(sess.User, job) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	resp := jobAPIResponse{ID: job.ID, Status: job.Status, Output: job.Output, Error: job.Error}
	if !job.CreatedAt.IsZero() && job.CreatedAt.Unix() > 0 {
		resp.Created = job.CreatedAt.Format(time.RFC3339)
	}
	if !job.StartedAt.IsZero() && job.StartedAt.Unix() > 0 {
		resp.Started = job.StartedAt.Format(time.RFC3339)
	}
	job.Sensitive = effectiveJobSensitive(job)
	if job.Sensitive && sess.User.Role != "admin" {
		resp.Output = "敏感命令输出仅管理员可见。"
		resp.Error = ""
	}
	if !job.FinishedAt.IsZero() && job.FinishedAt.Unix() > 0 {
		resp.Finished = job.FinishedAt.Format(time.RFC3339)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (s *Server) canAccessJob(user store.User, job store.Job) bool {
	return user.Role == "admin" || job.UserID == user.ID
}

func effectiveJobSensitive(job store.Job) bool {
	if job.Sensitive {
		return true
	}
	if job.Kind != "command" {
		return false
	}
	var args []string
	if json.Unmarshal([]byte(job.ArgsJSON), &args) != nil {
		return true
	}
	sensitive, _ := command.Sensitive(job.Tool, args)
	return sensitive
}

func jobHostLabel(job store.Job) string {
	if job.HostName != "" {
		return job.HostName
	}
	if job.HostID != "" {
		return "主机已删除或不可用"
	}
	return "未绑定主机（不会执行）"
}

func (s *Server) jobLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if store.IsNotFound(err) {
		s.renderError(w, r, http.StatusNotFound, "任务不存在", "该任务不存在或已被清理。")
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) consolePage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("host"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if err := libvirt.ValidateName(vmName); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "名称无效", err.Error())
		return
	}
	info, err := s.libvirt.DomainInfo(r.Context(), host.ID, vmName)
	if err != nil {
		s.logRequestError(r, "read console VM state", err)
		s.renderError(w, r, http.StatusBadGateway, "无法打开控制台", "无法读取虚拟机状态，请检查主机连接或查看服务日志。")
		return
	}
	state := info["State"]
	if allowed, accessErr := s.canUseConsole(r, host.ID, vmName); accessErr != nil || !allowed {
		s.renderError(w, r, http.StatusForbidden, "无权打开控制台", "当前账号没有该虚拟机的桌面授权。")
		return
	}
	if !consoleAvailable(state) {
		s.renderError(w, r, http.StatusConflict, "虚拟机未运行", "当前虚拟机状态为“"+state+"”，请先启动虚拟机再打开控制台。")
		return
	}
	con, err := s.libvirt.Console(r.Context(), host.ID, vmName)
	if err != nil {
		s.logRequestError(r, "read console configuration", err)
		s.renderError(w, r, http.StatusBadGateway, "无法打开控制台", "无法读取虚拟机控制台配置，请检查主机连接或查看服务日志。")
		return
	}
	sess, _ := sessionFrom(r.Context())
	wsToken, err := s.tokens.Issue(host.ID, vmName, sess.User.ID, consolepkg.ScopeWebSocket, time.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	passwordToken, err := s.tokens.Issue(host.ID, vmName, sess.User.ID, consolepkg.ScopePassword, time.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wsPath := "/console/" + url.PathEscape(host.ID) + "/" + url.PathEscape(vmName) + "/ws?token=" + url.QueryEscape(wsToken)
	passwordURL := "/console/" + url.PathEscape(host.ID) + "/" + url.PathEscape(vmName) + "/password?token=" + url.QueryEscape(passwordToken)
	s.render(w, r, "console", pageData{Title: "控制台 · " + vmName, Nav: "vms", Data: map[string]any{
		"HostID": host.ID, "HostName": host.Name, "VM": vmName, "WSURL": wsPath, "PasswordURL": passwordURL,
		"AllowInsecureConsole": s.cfg.AllowInsecureConsole, "ConsoleWarnings": consoleSecurityWarnings(con),
	}})
}

func (s *Server) consoleWS(w http.ResponseWriter, r *http.Request) {
	claims, host, token, ok := s.validateConsoleRequest(w, r, consolepkg.ScopeWebSocket)
	if !ok {
		return
	}
	if !s.acquireConsoleSlot() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "console capacity exhausted", http.StatusServiceUnavailable)
		return
	}
	defer s.releaseConsoleSlot()
	uri, err := libvirt.DecryptURI(s.cipher, host)
	if err != nil {
		s.logRequestError(r, "decrypt console host credentials", err)
		http.Error(w, "host credentials unavailable", http.StatusInternalServerError)
		return
	}
	con, err := s.libvirt.Console(r.Context(), host.ID, claims.VM)
	if err != nil {
		s.logRequestError(r, "read console websocket configuration", err)
		http.Error(w, "console backend unavailable", http.StatusBadGateway)
		return
	}
	backend, err := consolepkg.Dial(r.Context(), uri, con, consolepkg.DialOptions{
		SSHPath:          s.cfg.SSHBin,
		AllowInsecureVNC: s.cfg.AllowInsecureVNC,
		LoopbackAddress:  s.cfg.ConsoleLoopbackAddress,
	})
	if err != nil {
		s.logRequestError(r, "dial console backend", err)
		http.Error(w, "console backend unavailable", http.StatusBadGateway)
		return
	}
	defer s.trackConsoleBackend(backend)()
	// Consume the bearer token only after the backend is reachable. Capacity
	// exhaustion, a stopped VM or a broken SSH tunnel no longer burns the token
	// and force the operator to reload the page.
	if !s.claimConsoleToken(w, r, token, claims) {
		_ = backend.Close()
		return
	}
	s.consoleSessions.Add(1)
	defer s.consoleSessions.Add(-1)
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username,
		Action: "console.open", HostID: host.ID, VMName: claims.VM, RemoteIP: s.clientIP(r)})
	defer s.audit.Record(context.Background(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username,
		Action: "console.close", HostID: host.ID, VMName: claims.VM, RemoteIP: s.clientIP(r)})
	if err := s.proxy.Upgrade(w, r, backend); err != nil {
		s.log.Warn("console proxy closed", "error", err, "host", host.Name, "vm", claims.VM)
	}
}

func (s *Server) acquireConsoleSlot() bool {
	if s.consoleSlots == nil {
		return true
	}
	select {
	case s.consoleSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseConsoleSlot() {
	if s.consoleSlots != nil {
		<-s.consoleSlots
	}
}

func (s *Server) consolePassword(w http.ResponseWriter, r *http.Request) {
	claims, host, token, ok := s.validateConsoleRequest(w, r, consolepkg.ScopePassword)
	if !ok {
		return
	}
	con, err := s.libvirt.Console(r.Context(), host.ID, r.PathValue("vm"))
	if err != nil {
		s.logRequestError(r, "read console password", err)
		writeJSONError(w, http.StatusBadGateway, "console unavailable")
		return
	}
	if !s.claimConsoleToken(w, r, token, claims) {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if sess, ok := sessionFrom(r.Context()); ok {
		s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username,
			Action: "console.password_view", HostID: host.ID, VMName: claims.VM, RemoteIP: s.clientIP(r)})
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"password": con.Password})
}

func (s *Server) validateConsoleRequest(w http.ResponseWriter, r *http.Request, scope consolepkg.Scope) (consolepkg.Claims, store.Host, string, bool) {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	token := r.URL.Query().Get("token")
	claims, err := s.tokens.Validate(token, scope, consolepkg.Binding{
		HostID: r.PathValue("host"), VM: r.PathValue("vm"), UserID: sess.User.ID,
	}, time.Now())
	if err != nil {
		if errors.Is(err, consolepkg.ErrBindingMismatch) {
			writeJSONError(w, http.StatusForbidden, "console token does not match request")
			return consolepkg.Claims{}, store.Host{}, "", false
		}
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	if err := libvirt.ValidateName(claims.VM); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid VM name")
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	host, err := s.db.GetHost(r.Context(), claims.HostID)
	if err != nil {
		s.logRequestError(r, "load console host", err)
		writeJSONError(w, http.StatusBadGateway, "host unavailable")
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	if !host.Enabled {
		writeJSONError(w, http.StatusBadGateway, "host unavailable")
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	allowed, err := s.canUseConsole(r, host.ID, claims.VM)
	if err != nil || !allowed {
		writeJSONError(w, http.StatusForbidden, "console access denied")
		return consolepkg.Claims{}, store.Host{}, "", false
	}
	return claims, host, token, true
}

func (s *Server) claimConsoleToken(w http.ResponseWriter, r *http.Request, token string, claims consolepkg.Claims) bool {
	claimed, err := s.db.ClaimConsoleToken(r.Context(), auth.TokenHash(token), time.Unix(claims.Expiry, 0))
	if err != nil {
		s.serverError(w, r, err)
		return false
	}
	if !claimed {
		writeJSONError(w, http.StatusUnauthorized, "console token already used")
		return false
	}
	return true
}
