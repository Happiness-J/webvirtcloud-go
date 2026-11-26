package web

import "webvirtcloud-go/internal/libvirt"

// noCurrentSnapshot marks a VM that has snapshots but none of them is current.
// An empty list and a failed lookup stay unmarked. The result never selects
// a current snapshot or rolls one back.
func noCurrentSnapshot(snapshots []libvirt.Snapshot, err error) bool {
	if err != nil || len(snapshots) == 0 {
		return false
	}
	for _, snapshot := range snapshots {
		if snapshot.Current {
			return false
		}
	}
	return true
}
