package web

import (
	"context"
	"net/http"

	"webvirtcloud-go/internal/libvirt"
)

const deleteProtectionTag = "protected"

func (s *Server) vmDeleteProtectionSave(w http.ResponseWriter, r *http.Request) {
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
	if !s.vmVisibleOrDeny(w, r, host.ID, vmName) {
		return
	}
	enabled := r.Form.Get("enabled") == "1"
	if err := s.setVMDeleteProtection(r.Context(), host.ID, vmName, enabled); err != nil {
		s.redirectError(w, r, vmRedirect(host.ID, vmName), "无法保存防删锁")
		return
	}
	message := "防删锁已关闭"
	if enabled {
		message = "防删锁已开启"
	}
	http.Redirect(w, r, flashURL(vmRedirect(host.ID, vmName), message, "ok"), http.StatusSeeOther)
}

func (s *Server) vmDeleteProtected(ctx context.Context, hostID, vmName string) (bool, error) {
	tags, err := s.db.ListVMTags(ctx, hostID)
	if err != nil {
		return false, err
	}
	return vmHasDeleteProtection(tags, vmName), nil
}

func (s *Server) setVMDeleteProtection(ctx context.Context, hostID, vmName string, enabled bool) error {
	return s.db.SetVMTag(ctx, hostID, vmName, deleteProtectionTag, enabled)
}

func vmHasDeleteProtection(tags map[string][]string, vmName string) bool {
	for _, tag := range tags[vmName] {
		if tag == deleteProtectionTag {
			return true
		}
	}
	return false
}
