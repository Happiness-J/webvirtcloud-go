package web

import (
	"context"
	"path/filepath"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

type volumeUsage struct {
	VM     string
	Target string
}

func (s *Server) volumeUsages(ctx context.Context, hostID string, volumes []libvirt.Volume) map[string][]volumeUsage {
	out := map[string][]volumeUsage{}
	domains, err := s.libvirt.ListDomains(ctx, hostID)
	if err != nil {
		return out
	}
	for _, domain := range domains {
		raw, blockErr := s.libvirt.BlockDevices(ctx, hostID, domain.Name)
		if blockErr != nil {
			continue
		}
		for _, device := range libvirt.ParseBlockDevices(raw) {
			for _, volume := range volumes {
				if volumePathMatches(volume.Path, device.Source) {
					out[volume.Name] = append(out[volume.Name], volumeUsage{VM: domain.Name, Target: device.Target})
				}
			}
		}
	}
	return out
}

func volumePathMatches(volumePath, source string) bool {
	volumePath = strings.TrimSpace(volumePath)
	source = strings.TrimSpace(source)
	if volumePath == "" || source == "" || source == "-" {
		return false
	}
	return filepath.Clean(volumePath) == filepath.Clean(source)
}

func volumeUsageText(usages []volumeUsage) string {
	if len(usages) == 0 {
		return "未发现虚拟机引用"
	}
	parts := make([]string, 0, len(usages))
	for _, usage := range usages {
		parts = append(parts, usage.VM+" · "+usage.Target)
	}
	return strings.Join(parts, "，")
}
