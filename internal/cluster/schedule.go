// Package cluster decides where a virtual machine should run when an operator
// asks for placement or when its current host is no longer available.
package cluster

import (
	"fmt"
	"sort"
)

const (
	PolicySpread   = "spread"
	PolicyPack     = "pack"
	PolicyBalanced = "balanced"
)

// Host is the placement view of one compute node. Load fields are percentages.
type Host struct {
	ID          string
	Name        string
	URI         string
	Enabled     bool
	Maintenance bool
	Reachable   bool
	CPUPercent  float64
	MemPercent  float64
	RunningVMs  int
}

// Request describes one virtual machine that needs a destination.
type Request struct {
	VMName        string
	SourceHostID  string
	Policy        string
	RequireShared bool
}

// Decision is the chosen destination and why the others were skipped.
type Decision struct {
	HostID   string
	HostName string
	URI      string
	Reason   string
	Score    float64
}

// Place picks a surviving host. It never returns the source host, a disabled
// host, a host in maintenance, or a host that failed its last health probe.
// Shared storage is required for automatic restart: a local-only disk cannot
// be started on another node.
func Place(hosts []Host, req Request) (Decision, error) {
	policy := req.Policy
	if policy == "" {
		policy = PolicyBalanced
	}
	if policy != PolicySpread && policy != PolicyPack && policy != PolicyBalanced {
		return Decision{}, fmt.Errorf("未知放置策略")
	}
	if req.RequireShared && req.SourceHostID == "" {
		return Decision{}, fmt.Errorf("自动重启需要记录原主机")
	}
	candidates := make([]Host, 0, len(hosts))
	for _, host := range hosts {
		if !eligible(host, req.SourceHostID) {
			continue
		}
		if !migratableURI(host.URI) {
			continue
		}
		candidates = append(candidates, host)
	}
	if len(candidates) == 0 {
		return Decision{}, fmt.Errorf("没有可接收虚拟机的存活主机")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return rank(candidates[i], policy) < rank(candidates[j], policy)
	})
	chosen := candidates[0]
	return Decision{
		HostID:   chosen.ID,
		HostName: chosen.Name,
		URI:      chosen.URI,
		Score:    rank(chosen, policy),
		Reason:   reason(policy, chosen),
	}, nil
}

func eligible(host Host, sourceID string) bool {
	if host.ID == "" || host.ID == sourceID || !host.Enabled || host.Maintenance || !host.Reachable {
		return false
	}
	if host.CPUPercent < 0 || host.CPUPercent > 100 || host.MemPercent < 0 || host.MemPercent > 100 || host.RunningVMs < 0 {
		return false
	}
	return true
}

func migratableURI(uri string) bool {
	return len(uri) > len("qemu+ssh://") && (hasPrefix(uri, "qemu+ssh://") || hasPrefix(uri, "qemu+tls://")) && !containsSpace(uri)
}

func rank(host Host, policy string) float64 {
	load := host.CPUPercent*0.6 + host.MemPercent*0.4
	switch policy {
	case PolicyPack:
		// Lower rank wins. A busier host sorts first until it is nearly full.
		if load >= 90 {
			return 10000 + load
		}
		return 1000 - load
	case PolicySpread:
		return float64(host.RunningVMs)*1000 + load
	default:
		return load*100 + float64(host.RunningVMs)
	}
}

func reason(policy string, host Host) string {
	switch policy {
	case PolicyPack:
		return fmt.Sprintf("紧凑放置：优先使用已有负载的 %s", host.Name)
	case PolicySpread:
		return fmt.Sprintf("分散放置：%s 当前运行的虚拟机最少", host.Name)
	default:
		return fmt.Sprintf("均衡放置：%s 的 CPU 与内存余量最大", host.Name)
	}
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func containsSpace(value string) bool {
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}
