package web

import (
	"fmt"
	"net/http"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

func vmManifest(detail libvirt.DomainDetail) string {
	var lines []string
	lines = append(lines, "name: "+safeManifest(detail.Domain.Name))
	lines = append(lines, "state: "+safeManifest(detail.Domain.State))
	lines = append(lines, "vcpus: "+fmt.Sprintf("%d", detail.Domain.VCPUs))
	lines = append(lines, "memory_kib: "+fmt.Sprintf("%d", detail.Domain.MaxMemoryKiB))
	for _, device := range detail.BlockList {
		lines = append(lines, "disk: "+safeManifest(device.Target)+" "+safeManifest(device.Source))
	}
	for _, address := range detail.Addresses {
		lines = append(lines, "address: "+safeManifest(address.Interface)+" "+safeManifest(address.Address))
	}
	return strings.Join(lines, "\n") + "\n"
}

func safeManifest(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.Contains(lower, "://") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key") {
		return "[已隐藏]"
	}
	return strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
}

func (s *Server) vmManifest(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if !s.vmVisibleOrDeny(w, r, host.ID, vmName) {
		return
	}
	detail, err := s.libvirt.Detail(r.Context(), host.ID, vmName, false)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=vm-manifest.txt")
	_, _ = w.Write([]byte(vmManifest(detail)))
}
