package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestStaleProbeHostsKeepsOldChecksOnly(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	hosts := []store.Host{{ID: "old", Name: "计算甲"}, {ID: "fresh", Name: "计算乙"}, {ID: "none", Name: "计算丙"}}
	health := []store.HostHealth{
		{HostID: "old", CheckedAt: now.Add(-50 * time.Hour)},
		{HostID: "fresh", CheckedAt: now.Add(-2 * time.Hour)},
	}
	got := staleProbeHosts(hosts, health, now)
	if len(got) != 1 || got[0].HostName != "计算甲" || got[0].When != "早于两天" {
		t.Fatalf("stale = %#v", got)
	}
}

func TestDashboardRendersStaleProbes(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "dashboard", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: dashboardData{
		Hosts:       []store.Host{{ID: "old", Name: "计算甲", Enabled: true}},
		StaleProbes: []staleProbeHost{{HostID: "old", HostName: "计算甲", When: "早于一天"}},
	}}
	var rendered strings.Builder
	if err := templates["dashboard"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"探活已过期", "计算甲", "早于一天", "不重新探活", "/hosts/old"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
