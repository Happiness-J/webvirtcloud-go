package web

import "net/http"

func (s *Server) hostKSM(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	enabled := r.Form.Get("enabled")
	if enabled != "0" && enabled != "1" {
		s.redirectError(w, r, "/hosts/"+host.ID, "KSM 开关参数无效")
		return
	}
	sess, _ := sessionFrom(r.Context())
	args := []string{"node-memory-tune", "shm-merge", enabled}
	if _, err := s.enqueueCommand(r, sess.User, host.ID, "host-action", "设置 KSM", "virsh", args, "", 60); err != nil {
		s.redirectError(w, r, "/hosts/"+host.ID, err.Error())
		return
	}
	http.Redirect(w, r, "/hosts/"+host.ID, http.StatusSeeOther)
}
