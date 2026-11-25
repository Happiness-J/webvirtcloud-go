package web

import (
	"path"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// duplicateDisk is one path attached more than once on the same VM.
// The list is read-only and never detaches a disk.
type duplicateDisk struct {
	Name    string
	Targets []string
}

func duplicateDiskMounts(devices []libvirt.BlockDevice) []duplicateDisk {
	order := []string{}
	targets := map[string][]string{}
	names := map[string]string{}
	for _, device := range devices {
		if !strings.EqualFold(device.Device, "disk") {
			continue
		}
		source := strings.TrimSpace(device.Source)
		if source == "" || source == "-" {
			continue
		}
		key := path.Clean(strings.ReplaceAll(source, "\\", "/"))
		if _, ok := targets[key]; !ok {
			order = append(order, key)
			names[key] = path.Base(key)
		}
		targets[key] = append(targets[key], device.Target)
	}
	var out []duplicateDisk
	for _, key := range order {
		if len(targets[key]) < 2 {
			continue
		}
		out = append(out, duplicateDisk{Name: names[key], Targets: targets[key]})
	}
	return out
}
