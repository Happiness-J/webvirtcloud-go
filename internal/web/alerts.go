package web

import (
	"context"

	"webvirtcloud-go/internal/store"
	"webvirtcloud-go/internal/webhook"
)

func (s *Server) evaluateAlerts(ctx context.Context, host store.Host) {
	if s.hooks == nil || !s.hooks.Enabled() {
		return
	}
	rules, err := s.db.ListAlertRules(ctx, host.ID)
	if err != nil || len(rules) == 0 {
		return
	}
	stats, err := s.libvirt.DomainStats(ctx, host.ID)
	if err != nil {
		return
	}
	for _, rule := range rules {
		for _, item := range stats {
			if rule.VMName != "" && rule.VMName != item.Name {
				continue
			}
			value := 0.0
			switch rule.Metric {
			case "memory":
				used, ok := guestMemoryUsedKiB(item)
				if !ok {
					continue
				}
				value = float64(memoryUsagePercent(used, item.BalloonMaximum))
			case "cpu":
				continue
			}
			if value >= rule.Threshold {
				s.hooks.Send(webhook.Event{Action: "resource.threshold", HostID: host.ID, VM: item.Name, Detail: map[string]any{"metric": rule.Metric, "value": value, "threshold": rule.Threshold}})
			}
		}
	}
}
