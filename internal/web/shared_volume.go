package web

import (
	"path"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// sharedVolume is one disk path attached to more than one VM. It is
// read-only and never detaches a disk.
type sharedVolume struct {
	Name string
	Path string
	VMs  []string
}

func sharedVolumePaths(volumes []libvirt.Volume, usage map[string][]volumeUsage) []sharedVolume {
	var out []sharedVolume
	for _, volume := range volumes {
		items := usage[volume.Name]
		seen := map[string]bool{}
		var names []string
		for _, item := range items {
			if item.VM == "" || seen[item.VM] {
				continue
			}
			seen[item.VM] = true
			names = append(names, item.VM)
		}
		if len(names) < 2 {
			continue
		}
		out = append(out, sharedVolume{Name: volume.Name, Path: path.Base(strings.ReplaceAll(volume.Path, "\\", "/")), VMs: names})
	}
	return out
}
