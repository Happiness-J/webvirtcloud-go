package web

import (
	"net/http"
	"strings"

	"webvirtcloud-go/internal/store"
)

func (s *Server) ipPoolCreate(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if !s.hostVisibleOrDeny(w, r, hostID) {
		return
	}
	pool := store.IPPool{HostID: hostID, Name: strings.TrimSpace(r.Form.Get("name")), CIDR: strings.TrimSpace(r.Form.Get("cidr")), Floating: r.Form.Get("floating") == "yes"}
	if err := s.db.CreateIPPool(r.Context(), pool); err != nil {
		message := "无法创建地址池"
		switch err.Error() {
		case "地址池网段无效", "地址池名称无效":
			message = err.Error()
		}
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", message)
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "地址池已创建", "ok"), http.StatusSeeOther)
}

func (s *Server) ipPoolDelete(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if !s.hostVisibleOrDeny(w, r, hostID) {
		return
	}
	poolID := strings.TrimSpace(r.Form.Get("pool_id"))
	if err := s.db.DeleteIPPool(r.Context(), hostID, poolID); err != nil {
		message := "无法删除地址池"
		if err.Error() == "地址池不存在" {
			message = err.Error()
		}
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", message)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "ip_pool.deleted", HostID: hostID, RemoteIP: s.clientIP(r), Detail: map[string]any{"pool_id": poolID}})
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "地址池已删除", "ok"), http.StatusSeeOther)
}

func (s *Server) ipAllocate(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if !s.hostVisibleOrDeny(w, r, hostID) {
		return
	}
	if !s.ipPoolOnHostOrDeny(w, r, hostID, r.Form.Get("pool_id")) {
		return
	}
	address, err := s.db.AllocateIP(r.Context(), r.Form.Get("pool_id"), strings.TrimSpace(r.Form.Get("owner")))
	if err != nil {
		message := "无法分配地址"
		if err.Error() == "地址池已满" {
			message = err.Error()
		}
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", message)
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "已分配 "+address, "ok"), http.StatusSeeOther)
}

func (s *Server) ipRelease(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if !s.hostVisibleOrDeny(w, r, hostID) {
		return
	}
	if !s.ipPoolOnHostOrDeny(w, r, hostID, r.Form.Get("pool_id")) {
		return
	}
	if err := s.db.ReleaseIP(r.Context(), r.Form.Get("pool_id"), strings.TrimSpace(r.Form.Get("address"))); err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", "无法释放地址")
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "地址已释放", "ok"), http.StatusSeeOther)
}

func (s *Server) ipPoolOnHostOrDeny(w http.ResponseWriter, r *http.Request, hostID, poolID string) bool {
	ok, err := s.db.IPPoolOnHost(r.Context(), hostID, strings.TrimSpace(poolID))
	if err != nil {
		s.serverError(w, r, err)
		return false
	}
	if !ok {
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", "地址池不属于当前主机")
		return false
	}
	return true
}
