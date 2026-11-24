package web

import (
	"testing"
	"time"

	"webvirtcloud-go/internal/cluster"
	"webvirtcloud-go/internal/store"
)

func TestPlacementSuggestionPicksLowerLoad(t *testing.T) {
	hosts := []store.Host{{ID: "a", Name: "busy", Enabled: true}, {ID: "b", Name: "idle", Enabled: true}}
	health := map[string]store.HostHealth{
		"a": {HostID: "a", Reachable: true, CPUPercent: 80, MemPercent: 70, RunningVMs: 4, CheckedAt: time.Now()},
		"b": {HostID: "b", Reachable: true, CPUPercent: 10, MemPercent: 20, RunningVMs: 1, CheckedAt: time.Now()},
	}
	got := placementSuggestion(hosts, health, cluster.PolicyBalanced)
	if got.HostID != "b" || got.URI != "" || got.Reason == "" {
		t.Fatalf("suggestion = %#v", got)
	}
}
