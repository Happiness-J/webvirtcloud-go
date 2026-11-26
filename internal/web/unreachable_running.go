package web

import "webvirtcloud-go/internal/store"

// unreachableRunningHost is a host whose latest probe failed while it still
// had running VMs. The list only names the host and the count. It never
// evacuates or restarts anything.
type unreachableRunningHost struct {
	HostID   string
	HostName string
	Running  int
}

func unreachableRunningHosts(hosts []store.Host, health []store.HostHealth) []unreachableRunningHost {
	byID := make(map[string]store.HostHealth, len(health))
	for _, item := range health {
		byID[item.HostID] = item
	}
	var out []unreachableRunningHost
	for _, host := range hosts {
		item, ok := byID[host.ID]
		if !ok || item.Reachable || item.RunningVMs < 1 || item.CheckedAt.IsZero() {
			continue
		}
		out = append(out, unreachableRunningHost{HostID: host.ID, HostName: host.Name, Running: item.RunningVMs})
	}
	return out
}
