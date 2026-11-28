package web

import (
	"encoding/xml"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// unbackedDisk is a data disk that no checkpoint names. The list is a notice
// only and never creates a checkpoint.
type unbackedDisk struct {
	Target string
	Source string
}

func disksOutsideCheckpoints(devices []libvirt.BlockDevice, checkpointXML []string) []unbackedDisk {
	covered := map[string]bool{}
	for _, item := range checkpointXML {
		for _, name := range checkpointDiskNames(item) {
			covered[name] = true
		}
	}
	var out []unbackedDisk
	for _, device := range devices {
		if !strings.EqualFold(device.Device, "disk") || device.Target == "" || covered[device.Target] {
			continue
		}
		out = append(out, unbackedDisk{Target: device.Target, Source: device.Source})
	}
	return out
}

func checkpointDiskNames(value string) []string {
	var parsed struct {
		Disks []struct {
			Name string `xml:"name,attr"`
		} `xml:"disks>disk"`
	}
	if err := xml.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	var names []string
	for _, disk := range parsed.Disks {
		if disk.Name != "" {
			names = append(names, disk.Name)
		}
	}
	return names
}
