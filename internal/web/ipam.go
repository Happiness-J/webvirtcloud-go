package web

import (
	"net/http"
	"strings"

	"webvirtcloud-go/internal/store"
)

func (s *Server) ipPoolCreate(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	pool := store.IPPool{HostID: hostID, Name: strings.TrimSpace(r.Form.Get("name")), CIDR: strings.TrimSpace(r.Form.Get("cidr")), Floating: r.Form.Get("floating") == "yes"}
	if err := s.db.CreateIPPool(r.Context(), pool); err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", err.Error())
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "地址池已创建", "ok"), http.StatusSeeOther)
}

func (s *Server) ipAllocate(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	address, err := s.db.AllocateIP(r.Context(), r.Form.Get("pool_id"), strings.TrimSpace(r.Form.Get("owner")))
	if err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", err.Error())
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "已分配 "+address, "ok"), http.StatusSeeOther)
}

func (s *Server) ipRelease(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if err := s.db.ReleaseIP(r.Context(), r.Form.Get("pool_id"), strings.TrimSpace(r.Form.Get("address"))); err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/networks", err.Error())
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+hostID+"/networks", "地址已释放", "ok"), http.StatusSeeOther)
}
