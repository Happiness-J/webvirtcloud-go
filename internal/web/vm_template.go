package web

import (
	"net/http"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/store"
)

func (s *Server) vmTemplateSave(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.Form.Get("template_name"))
	vcpus, cpuErr := strconv.Atoi(r.Form.Get("vcpus"))
	memory, memoryErr := strconv.Atoi(r.Form.Get("memory"))
	disk, diskErr := strconv.Atoi(r.Form.Get("disk"))
	if cpuErr != nil || memoryErr != nil || diskErr != nil {
		s.redirectError(w, r, "/hosts/"+r.PathValue("id")+"/vms/new", "模板容量无效")
		return
	}
	item := store.VMTemplate{
		Name: name, VCPUs: vcpus, MemoryGB: memory, DiskGB: disk,
		Network: strings.TrimSpace(r.Form.Get("network")), OSVariant: strings.TrimSpace(r.Form.Get("os_profile")),
		Hostname: strings.TrimSpace(r.Form.Get("hostname")), Username: strings.TrimSpace(r.Form.Get("cloud_user")),
		Address: strings.TrimSpace(r.Form.Get("guest_address")), Gateway: strings.TrimSpace(r.Form.Get("guest_gateway")), DNS: strings.TrimSpace(r.Form.Get("guest_dns")),
	}
	if err := s.db.SaveVMTemplate(r.Context(), item); err != nil {
		s.redirectError(w, r, "/hosts/"+r.PathValue("id")+"/vms/new", "无法保存模板")
		return
	}
	http.Redirect(w, r, flashURL("/hosts/"+r.PathValue("id")+"/vms/new", "模板已保存", "ok"), http.StatusSeeOther)
}
