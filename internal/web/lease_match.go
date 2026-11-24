package web

import (
	"net/netip"
	"strings"

	"webvirtcloud-go/internal/store"
)

// addressUse is one read-only row joining a DHCP lease with an address-pool
// allocation. It never releases or reallocates an address.
type addressUse struct {
	Address string
	Lease   string
	Pool    string
	Owner   string
	Kind    string
}

func matchAddressUses(leaseText string, pools []store.IPPool, allocations map[string][]store.IPAllocation) []addressUse {
	leases := parseLeaseAddresses(leaseText)
	byAddr := map[string]addressUse{}
	order := []string{}
	for _, lease := range leases {
		key := canonicalAddress(lease.Address)
		if key == "" {
			continue
		}
		item := byAddr[key]
		if item.Address == "" {
			order = append(order, key)
		}
		item.Address = key
		item.Lease = lease.Client
		byAddr[key] = item
	}
	names := map[string]string{}
	for _, pool := range pools {
		names[pool.ID] = pool.Name
	}
	for poolID, items := range allocations {
		for _, item := range items {
			key := canonicalAddress(item.Address)
			if key == "" {
				continue
			}
			row := byAddr[key]
			if row.Address == "" {
				order = append(order, key)
			}
			row.Address = key
			row.Pool = names[poolID]
			row.Owner = item.Owner
			byAddr[key] = row
		}
	}
	out := make([]addressUse, 0, len(order))
	for _, key := range order {
		row := byAddr[key]
		switch {
		case row.Lease != "" && row.Owner != "":
			row.Kind = "两边都有"
		case row.Lease != "":
			row.Kind = "只有租约"
		default:
			row.Kind = "只有地址池"
		}
		out = append(out, row)
	}
	return out
}

type parsedLease struct {
	Address string
	Client  string
}

func parseLeaseAddresses(text string) []parsedLease {
	var out []parsedLease
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		var addr, client string
		for _, field := range fields {
			if ip, err := netip.ParseAddr(strings.Split(field, "/")[0]); err == nil && addr == "" && ip.IsValid() {
				addr = ip.String()
				continue
			}
		}
		if addr == "" {
			continue
		}
		var mac string
		for _, field := range fields {
			if isMAC(field) {
				mac = field
				continue
			}
			if field == addr || canonicalAddress(field) != "" || field == "-" || strings.Contains(field, "/") || isNumber(field) || isProtocol(field) {
				continue
			}
			client = field
			break
		}
		if client == "" {
			client = mac
		}
		if client == "" {
			client = "未标明"
		}
		out = append(out, parsedLease{Address: addr, Client: client})
	}
	return out
}

func canonicalAddress(value string) string {
	ip, err := netip.ParseAddr(strings.Split(strings.TrimSpace(value), "/")[0])
	if err != nil || !ip.IsValid() {
		return ""
	}
	return ip.String()
}

func isMAC(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
	}
	return true
}

func isNumber(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isProtocol(value string) bool {
	switch strings.ToLower(value) {
	case "ipv4", "ipv6", "protocol":
		return true
	default:
		return false
	}
}
