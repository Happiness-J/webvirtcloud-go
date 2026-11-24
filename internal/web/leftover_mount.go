package web

import (
	"path"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// leftoverMount is one attachment that would still be present on the next
// boot. The list is read-only and never ejects or detaches a device.
type leftoverMount struct {
	Kind  string
	Where string
	What  string
}

func shutdownLeftovers(state string, media []cdromMedia, devices []libvirt.AssignedDevice) []leftoverMount {
	if !strings.EqualFold(strings.TrimSpace(state), "shut off") {
		return nil
	}
	var out []leftoverMount
	for _, item := range media {
		if item.Empty {
			continue
		}
		out = append(out, leftoverMount{Kind: "光驱", Where: item.Target, What: item.Image})
	}
	for _, device := range devices {
		switch device.Kind {
		case "usb":
			out = append(out, leftoverMount{Kind: "USB", Where: "主机设备", What: deviceLabel(device)})
		case "block":
			out = append(out, leftoverMount{Kind: "宿主机磁盘", Where: "主机设备", What: path.Base(strings.ReplaceAll(device.Source, "\\", "/"))})
		}
	}
	return out
}

func deviceLabel(device libvirt.AssignedDevice) string {
	if strings.TrimSpace(device.Description) != "" {
		return device.Description
	}
	return device.Source
}
