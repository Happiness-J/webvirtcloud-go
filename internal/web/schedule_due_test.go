package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestSchedulesDueSoonKeepsTheNextWeek(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	hosts := []store.Host{{ID: "host", Name: "compute"}, {ID: "other", Name: "other"}}
	items := []store.VMSchedule{
		{HostID: "host", VMName: "web", Kind: "snapshot", Enabled: true, NextRunAt: now.Add(2 * 24 * time.Hour)},
		{HostID: "host", VMName: "db", Kind: "power", PowerAction: "shutdown", Enabled: true, NextRunAt: now.Add(-time.Hour)},
		{HostID: "host", VMName: "later", Kind: "snapshot", Enabled: true, NextRunAt: now.Add(8 * 24 * time.Hour)},
		{HostID: "host", VMName: "off", Kind: "power", PowerAction: "start", Enabled: false, NextRunAt: now.Add(time.Hour)},
		{HostID: "hidden", VMName: "secret", Kind: "snapshot", Enabled: true, NextRunAt: now.Add(time.Hour)},
	}
	got := schedulesDueSoon(items, hosts, now)
	if len(got) != 2 || got[0].Label != "定时快照" || got[0].When != "七天内" || got[1].Label != "定时关机" || got[1].When != "已到时间" {
		t.Fatalf("due = %#v", got)
	}
}

func TestJobsPageRendersDueSchedules(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "jobs", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Jobs": []store.Job{}, "Stats": store.QueueStats{}, "Page": 1, "HasNext": false,
		"FailureGroups": []failureCategory{}, "FailureTargets": []failureTarget{},
		"DueSchedules": []dueSchedule{{HostID: "host", HostName: "compute", VMName: "web", Label: "定时快照", When: "一天内"}},
	}}
	var rendered strings.Builder
	if err := templates["jobs"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"即将到期", "定时快照", "web", "一天内", "不提前执行", "/hosts/host/vms/web"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
