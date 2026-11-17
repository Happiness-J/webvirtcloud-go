package web

import (
	"net/http"
	"strconv"

	"webvirtcloud-go/internal/store"
)

func (s *Server) userQuotaSave(w http.ResponseWriter, r *http.Request) {
	target, err := s.db.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	vms, vErr := strconv.Atoi(r.Form.Get("max_vms"))
	cpus, cErr := strconv.Atoi(r.Form.Get("max_vcpus"))
	memory, mErr := strconv.Atoi(r.Form.Get("max_memory_gb"))
	disk, dErr := strconv.Atoi(r.Form.Get("max_disk_gb"))
	if vErr != nil || cErr != nil || mErr != nil || dErr != nil {
		s.redirectError(w, r, "/users/"+target.ID+"/access", "配额必须是整数")
		return
	}
	if err := s.db.SaveUserQuota(r.Context(), store.UserQuota{UserID: target.ID, MaxVMs: vms, MaxVCPUs: cpus, MaxMemoryGB: memory, MaxDiskGB: disk}); err != nil {
		message := "无法保存资源配额"
		if err.Error() == "配额不能为负数" {
			message = err.Error()
		}
		s.redirectError(w, r, "/users/"+target.ID+"/access", message)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "user.quota", RemoteIP: s.clientIP(r), Detail: map[string]any{"user_id": target.ID}})
	http.Redirect(w, r, flashURL("/users/"+target.ID+"/access", "资源配额已保存", "ok"), http.StatusSeeOther)
}
