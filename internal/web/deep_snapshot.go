package web

import "webvirtcloud-go/internal/libvirt"

const deepSnapshotDepth = 5

// deepSnapshot is a VM whose snapshot tree reaches five levels. The list
// never merges or deletes a snapshot.
type deepSnapshot struct {
	HostID   string
	HostName string
	Name     string
	Depth    int
}

func deepSnapshotChains(hostID, hostName string, domains []libvirt.Domain, snapshots map[string][]libvirt.Snapshot) []deepSnapshot {
	var out []deepSnapshot
	for _, domain := range domains {
		items, ok := snapshots[domain.Name]
		if !ok || domain.Name == "" {
			continue
		}
		depth := summarizeSnapshots(items).Depth
		if depth < deepSnapshotDepth {
			continue
		}
		out = append(out, deepSnapshot{HostID: hostID, HostName: hostName, Name: domain.Name, Depth: depth})
	}
	return out
}
