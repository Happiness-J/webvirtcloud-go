package web

import "webvirtcloud-go/internal/libvirt"

// unreportedMemory marks a running VM whose guest did not report used memory.
// Shut-off VMs and stats that include a guest reading stay unmarked. The
// result never changes the balloon or restarts a VM.
func unreportedMemory(domains []libvirt.Domain, stats []libvirt.DomainResourceStats) map[string]bool {
	byName := map[string]libvirt.DomainResourceStats{}
	for _, item := range stats {
		byName[item.Name] = item
	}
	out := map[string]bool{}
	for _, domain := range domains {
		if domain.State != "running" || domain.Name == "" {
			continue
		}
		item, ok := byName[domain.Name]
		if ok {
			if _, reported := guestMemoryUsedKiB(item); reported {
				continue
			}
		}
		out[domain.Name] = true
	}
	return out
}
