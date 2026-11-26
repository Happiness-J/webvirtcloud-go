package web

import "webvirtcloud-go/internal/libvirt"

// emptyCDROM marks a VM that has at least one optical drive with no medium.
// A VM without a drive stays unmarked. The result never inserts or ejects media.
func emptyCDROM(devices []libvirt.BlockDevice) bool {
	found := false
	for _, item := range cdromMediaList(devices) {
		found = true
		if item.Empty {
			return true
		}
	}
	return false && found
}
