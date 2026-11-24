package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"webvirtcloud-go/internal/cluster"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func (s *Server) clusterHostError(w http.ResponseWriter, r *http.Request, err error) {
	if store.IsNotFound(err) {
		s.redirectError(w, r, "/cluster", "主机不存在")
		return
	}
	s.logRequestError(r, "load host for cluster protection", err)
	s.redirectError(w, r, "/cluster", "读取主机失败，请稍后重试")
}

func (s *Server) clusterPage(w http.ResponseWriter, r *http.Request) {
	policy, err := s.db.ClusterPolicy(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	healthRows, err := s.db.ListHostHealth(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	health := map[string]store.HostHealth{}
	for _, item := range healthRows {
		health[item.HostID] = item
	}
	protected, err := s.db.ListProtectedVMs(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	suggestion := placementSuggestion(hosts, health, policy.Policy)
	since := time.Now().Add(-store.HostCapacityRetention)
	trends := map[string]hostCapacityTrend{}
	for _, host := range hosts {
		samples, err := s.db.ListHostCapacity(r.Context(), host.ID, since)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		trends[host.ID] = summarizeCapacity(samples)
	}
	s.render(w, r, "cluster", pageData{Title: "高可用与调度", Nav: "cluster", Data: map[string]any{
		"Policy": policy, "Hosts": hosts, "Health": health, "Protected": protected, "Suggestion": suggestion, "Trends": trends, "ProtectionGaps": s.protectionGapLabels(r, protected),
	}})
}

type hostCapacityTrend struct {
	Points                         int
	LatestCPU, LatestMem           float64
	PeakCPU, PeakMem               float64
	MinVMs, MaxVMs                 int
	CPUPoints, MemPoints, VMPoints string
}

func summarizeCapacity(samples []store.HostCapacitySample) hostCapacityTrend {
	trend := hostCapacityTrend{MinVMs: -1}
	var cpu, mem, vms []string
	maxVMs := 0
	for _, sample := range samples {
		if sample.RunningVMs > maxVMs {
			maxVMs = sample.RunningVMs
		}
	}
	scale := func(value, max float64) int {
		if max <= 0 {
			return 46
		}
		y := 46 - int(value/max*40)
		if y < 6 {
			return 6
		}
		if y > 46 {
			return 46
		}
		return y
	}
	for i, sample := range samples {
		trend.Points++
		trend.LatestCPU, trend.LatestMem = sample.CPUPercent, sample.MemPercent
		if sample.CPUPercent > trend.PeakCPU {
			trend.PeakCPU = sample.CPUPercent
		}
		if sample.MemPercent > trend.PeakMem {
			trend.PeakMem = sample.MemPercent
		}
		if trend.MinVMs < 0 || sample.RunningVMs < trend.MinVMs {
			trend.MinVMs = sample.RunningVMs
		}
		if sample.RunningVMs > trend.MaxVMs {
			trend.MaxVMs = sample.RunningVMs
		}
		x := "4"
		if len(samples) > 1 {
			x = strconv.Itoa(4 + i*232/(len(samples)-1))
		}
		cpu = append(cpu, x+","+strconv.Itoa(scale(sample.CPUPercent, 100)))
		mem = append(mem, x+","+strconv.Itoa(scale(sample.MemPercent, 100)))
		vms = append(vms, x+","+strconv.Itoa(scale(float64(sample.RunningVMs), float64(maxVMs))))
	}
	if trend.MinVMs < 0 {
		trend.MinVMs = 0
	}
	trend.CPUPoints = strings.Join(cpu, " ")
	trend.MemPoints = strings.Join(mem, " ")
	trend.VMPoints = strings.Join(vms, " ")
	return trend
}

func (s *Server) vmHAProtected(r *http.Request, hostID, vmName string) bool {
	items, err := s.db.ListProtectedVMs(r.Context())
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.HostID == hostID && item.VMName == vmName && item.Protected {
			return true
		}
	}
	return false
}

func (s *Server) protectionGapLabels(r *http.Request, protected []store.HAVM) map[string]string {
	out := map[string]string{}
	tagsByHost := map[string]map[string][]string{}
	for _, item := range protected {
		tags, ok := tagsByHost[item.HostID]
		if !ok {
			loaded, err := s.db.ListVMTags(r.Context(), item.HostID)
			if err != nil {
				continue
			}
			tags = loaded
			tagsByHost[item.HostID] = tags
		}
		gap := protectionMismatch(vmHasDeleteProtection(tags, item.VMName), item.Protected)
		if gap.Label != "" {
			out[item.HostID+"/"+item.VMName] = gap.Label
		}
	}
	return out
}

func (s *Server) clusterPolicySave(w http.ResponseWriter, r *http.Request) {
	failAfter, err := strconv.Atoi(r.Form.Get("fail_after"))
	if err != nil {
		s.redirectError(w, r, "/cluster", "故障确认次数无效")
		return
	}
	item := store.ClusterPolicy{
		Enabled:   r.Form.Get("enabled") == "1",
		Policy:    r.Form.Get("policy"),
		FailAfter: failAfter,
	}
	if err := s.db.SaveClusterPolicy(r.Context(), item); err != nil {
		s.logRequestError(r, "save cluster policy", err)
		s.redirectError(w, r, "/cluster", "保存调度策略失败，请稍后重试")
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "cluster.policy", RemoteIP: s.clientIP(r), Detail: map[string]any{"enabled": item.Enabled, "policy": item.Policy, "fail_after": item.FailAfter}})
	http.Redirect(w, r, "/cluster", http.StatusSeeOther)
}

func (s *Server) clusterProtect(w http.ResponseWriter, r *http.Request) {
	hostID := strings.TrimSpace(r.Form.Get("host_id"))
	vm := strings.TrimSpace(r.Form.Get("vm"))
	if err := libvirt.ValidateName(vm); err != nil {
		s.redirectError(w, r, "/cluster", "虚拟机名称无效")
		return
	}
	if _, err := s.db.GetHost(r.Context(), hostID); err != nil {
		s.clusterHostError(w, r, err)
		return
	}
	item := store.HAVM{HostID: hostID, VMName: vm, Protected: r.Form.Get("protected") == "1"}
	if err := s.db.SaveHAVM(r.Context(), item); err != nil {
		s.logRequestError(r, "save cluster protection", err)
		s.redirectError(w, r, "/cluster", "保存虚拟机保护失败，请稍后重试")
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "cluster.protect", HostID: hostID, RemoteIP: s.clientIP(r), Detail: map[string]any{"vm": vm, "protected": item.Protected}})
	http.Redirect(w, r, "/cluster", http.StatusSeeOther)
}

func placementSuggestion(hosts []store.Host, health map[string]store.HostHealth, policy string) cluster.Decision {
	views := make([]cluster.Host, 0, len(hosts))
	for _, host := range hosts {
		item := health[host.ID]
		views = append(views, cluster.Host{
			ID: host.ID, Name: host.Name, URI: "qemu+ssh://placement.invalid/system",
			Enabled: host.Enabled, Maintenance: host.Maintenance, Reachable: item.Reachable,
			CPUPercent: item.CPUPercent, MemPercent: item.MemPercent, RunningVMs: item.RunningVMs,
		})
	}
	decision, err := cluster.Place(views, cluster.Request{Policy: policy})
	if err != nil {
		return cluster.Decision{}
	}
	decision.URI = ""
	return decision
}
