package web

import (
	"net/http"
	"net/url"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

func (s *Server) vmTagSave(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	vmName := strings.TrimSpace(r.Form.Get("vm"))
	tag := strings.TrimSpace(r.Form.Get("tag"))
	if err := libvirt.ValidateName(vmName); err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/ops", "虚拟机名称无效")
		return
	}
	enabled := r.Form.Get("enabled") != "0"
	if err := s.db.SetVMTag(r.Context(), hostID, vmName, tag, enabled); err != nil {
		s.redirectError(w, r, "/hosts/"+hostID+"/ops", err.Error())
		return
	}
	http.Redirect(w, r, "/hosts/"+hostID+"/ops?tag="+url.QueryEscape(tag), http.StatusSeeOther)
}
