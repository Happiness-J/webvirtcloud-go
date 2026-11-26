package web

import (
	"time"

	"webvirtcloud-go/internal/store"
)

const probeStaleAfter = 24 * time.Hour

// staleProbeHost is a visible host whose latest health probe is older than a
// day. Hosts with no probe stay off the list. The notice never starts a probe.
type staleProbeHost struct {
	HostID   string
	HostName string
	When     string
}

func staleProbeHosts(hosts []store.Host, health []store.HostHealth, now time.Time) []staleProbeHost {
	byID := map[string]store.HostHealth{}
	for _, item := range health {
		byID[item.HostID] = item
	}
	var out []staleProbeHost
	for _, host := range hosts {
		item, ok := byID[host.ID]
		if !ok || item.CheckedAt.IsZero() || now.Sub(item.CheckedAt) < probeStaleAfter {
			continue
		}
		out = append(out, staleProbeHost{HostID: host.ID, HostName: host.Name, When: probeAge(item.CheckedAt, now)})
	}
	return out
}

func probeAge(checked, now time.Time) string {
	hours := int(now.Sub(checked).Hours())
	if hours < 48 {
		return "早于一天"
	}
	return "早于两天"
}
