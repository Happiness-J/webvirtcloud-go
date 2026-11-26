package web

import "webvirtcloud-go/internal/libvirt"

// unreferencedVolume is a volume in the open pool that no VM disk matches.
// The list is a notice only and never deletes a volume.
type unreferencedVolume struct {
	Name     string
	Capacity string
}

func unreferencedVolumes(volumes []libvirt.Volume, usage map[string][]volumeUsage) []unreferencedVolume {
	var out []unreferencedVolume
	for _, volume := range volumes {
		if volume.Name == "" || len(usage[volume.Name]) > 0 {
			continue
		}
		capacity := volume.Capacity
		if capacity == "" {
			capacity = volume.Allocation
		}
		out = append(out, unreferencedVolume{Name: volume.Name, Capacity: capacity})
	}
	return out
}
