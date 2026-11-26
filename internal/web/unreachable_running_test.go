package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestUnreachableRunningHostsKeepsFailedProbesWithGuests(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	hosts := []store.Host{{ID: "down", Name: "计算甲"}, {ID: "up", Name: "计算乙"}, {ID: "empty", Name: "计算丙"}, {ID: "unknown", Name: "计算丁"}}
	health := []store.HostHealth{
		{HostID: "down", Reachable: false, RunningVMs: 3, CheckedAt: now},
		{HostID: "up", Reachable: true, RunningVMs: 4, CheckedAt: now},
		{HostID: "empty", Reachable: false, RunningVMs: 0, CheckedAt: now},
	}
	got := unreachableRunningHosts(hosts, health)
	if len(got) != 1 || got[0].HostName != "计算甲" || got[0].Running != 3 {
		t.Fatalf("hosts = %#v", got)
	}
}

func TestDashboardRendersUnreachableRunningHosts(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "dashboard", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: dashboardData{
		Hosts:              []store.Host{{ID: "down", Name: "计算甲", Enabled: true}},
		UnreachableRunning: []unreachableRunningHost{{HostID: "down", HostName: "计算甲", Running: 2}},
	}}
	var rendered strings.Builder
	if err := templates["dashboard"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"失联仍在运行", "计算甲", "2", "不疏散", "/hosts/down"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
