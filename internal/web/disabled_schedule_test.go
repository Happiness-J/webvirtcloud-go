package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestDisabledSchedulesSkipsEnabledAndHiddenHosts(t *testing.T) {
	hosts := []store.Host{{ID: "host", Name: "compute"}}
	items := []store.VMSchedule{
		{HostID: "host", VMName: "web", Kind: "snapshot", Enabled: false},
		{HostID: "host", VMName: "db", Kind: "power", PowerAction: "start", Enabled: false},
		{HostID: "host", VMName: "live", Kind: "snapshot", Enabled: true},
		{HostID: "hidden", VMName: "secret", Kind: "power", PowerAction: "shutdown", Enabled: false},
	}
	got := disabledSchedules(items, hosts)
	if len(got) != 2 || got[0].Label != "定时快照" || got[1].Label != "定时开机" || got[1].VMName != "db" {
		t.Fatalf("disabled = %#v", got)
	}
}

func TestJobsPageRendersDisabledSchedules(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "jobs", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Jobs": []store.Job{}, "Stats": store.QueueStats{}, "Page": 1, "HasNext": false,
		"FailureGroups": []failureCategory{}, "FailureTargets": []failureTarget{},
		"DisabledSchedules": []disabledSchedule{{HostID: "host", HostName: "compute", VMName: "web", Label: "定时关机"}},
	}}
	var rendered strings.Builder
	if err := templates["jobs"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"未启用的定时任务", "定时关机", "web", "不启用", "不删除", "/hosts/host/vms/web"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
