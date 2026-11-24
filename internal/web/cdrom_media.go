package web

import (
	"path"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// cdromMedia is the read-only state of one optical drive. It does not eject
// or change the inserted image.
type cdromMedia struct {
	Target string
	Image  string
	Empty  bool
}

func cdromMediaList(devices []libvirt.BlockDevice) []cdromMedia {
	var out []cdromMedia
	for _, device := range devices {
		if !strings.EqualFold(device.Device, "cdrom") {
			continue
		}
		item := cdromMedia{Target: device.Target, Empty: mediaEmpty(device.Source)}
		if !item.Empty {
			item.Image = path.Base(strings.ReplaceAll(device.Source, "\\", "/"))
		}
		out = append(out, item)
	}
	return out
}

func mediaEmpty(source string) bool {
	value := strings.TrimSpace(source)
	return value == "" || value == "-"
}
