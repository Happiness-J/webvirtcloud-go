package web

import (
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// rebootPreview says which VMs would come back after a host reboot.
// It never reboots the host or changes autostart.
type rebootPreview struct {
	WillStart []libvirt.Domain
	StayDown  []libvirt.Domain
}

func buildRebootPreview(domains []libvirt.Domain) rebootPreview {
	var preview rebootPreview
	for _, domain := range domains {
		if autostartEnabled(domain.Autostart) {
			preview.WillStart = append(preview.WillStart, domain)
			continue
		}
		if powerOn(domain.State) {
			preview.StayDown = append(preview.StayDown, domain)
		}
	}
	return preview
}

func powerOn(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "paused", "active":
		return true
	default:
		return false
	}
}
