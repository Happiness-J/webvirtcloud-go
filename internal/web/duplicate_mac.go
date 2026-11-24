package web

import (
	"sort"
	"strings"
)

// duplicateMAC is one address used by two or more VMs on the same host.
// The list is read-only and never changes a NIC.
type duplicateMAC struct {
	MAC string
	VMs []string
}

type macOwner struct {
	VM  string
	MAC string
}

func duplicateMACs(owners []macOwner) []duplicateMAC {
	order := []string{}
	seen := map[string]map[string]bool{}
	for _, owner := range owners {
		mac := strings.ToLower(strings.TrimSpace(owner.MAC))
		if !validMAC(mac) || owner.VM == "" {
			continue
		}
		if seen[mac] == nil {
			seen[mac] = map[string]bool{}
			order = append(order, mac)
		}
		seen[mac][owner.VM] = true
	}
	var out []duplicateMAC
	for _, mac := range order {
		if len(seen[mac]) < 2 {
			continue
		}
		item := duplicateMAC{MAC: mac}
		for vm := range seen[mac] {
			item.VMs = append(item.VMs, vm)
		}
		sort.Strings(item.VMs)
		out = append(out, item)
	}
	return out
}
