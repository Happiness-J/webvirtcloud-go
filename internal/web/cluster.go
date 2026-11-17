package web

import (
	"net/http"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

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
	s.render(w, r, "cluster", pageData{Title: "高可用与调度", Nav: "cluster", Data: map[string]any{
		"Policy": policy, "Hosts": hosts, "Health": health, "Protected": protected,
	}})
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
		s.redirectError(w, r, "/cluster", err.Error())
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
		s.redirectError(w, r, "/cluster", "主机不存在")
		return
	}
	item := store.HAVM{HostID: hostID, VMName: vm, Protected: r.Form.Get("protected") == "1"}
	if err := s.db.SaveHAVM(r.Context(), item); err != nil {
		s.redirectError(w, r, "/cluster", err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "cluster.protect", HostID: hostID, RemoteIP: s.clientIP(r), Detail: map[string]any{"vm": vm, "protected": item.Protected}})
	http.Redirect(w, r, "/cluster", http.StatusSeeOther)
}
