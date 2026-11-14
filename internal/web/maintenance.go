package web

import (
	"net/http"
	"strings"
)

func (s *Server) hostMaintenance(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	enabled := r.Form.Get("enabled") == "1"
	if err := s.db.SetHostMaintenance(r.Context(), host.ID, enabled); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, "无法更新维护状态")
		return
	}
	http.Redirect(w, r, "/hosts/"+host.ID, http.StatusSeeOther)
}

func (s *Server) hostEvacuate(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	target := strings.TrimSpace(r.Form.Get("target"))
	domains, err := s.libvirt.ListDomains(r.Context(), host.ID)
	if err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, "无法读取需要疏散的虚拟机")
		return
	}
	sess, _ := sessionFrom(r.Context())
	for _, domain := range domains {
		if strings.ToLower(domain.State) != "running" {
			continue
		}
		args, err := migrationArgs(domain.Name, target, "", false)
		if err != nil {
			s.redirectError(w, r, "/hosts/"+host.ID, err.Error())
			return
		}
		if _, err := s.enqueueCommand(r, sess.User, host.ID, "vm-action", "疏散 "+domain.Name, "virsh", args, "", 3600); err != nil {
			s.redirectError(w, r, "/hosts/"+host.ID, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/jobs", http.StatusSeeOther)
}
