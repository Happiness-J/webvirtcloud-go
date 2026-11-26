package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestLatestFailedStartsKeepsNewestOutcome(t *testing.T) {
	jobs := []store.Job{
		{Label: "web: start", Status: "succeeded", HostID: "host", HostName: "compute", CreatedAt: time.Now()},
		{Label: "web: start", Status: "failed", HostID: "host", HostName: "compute"},
		{Label: "db: start", Status: "failed", HostID: "host", HostName: "compute"},
		{Label: "db: shutdown", Status: "failed", HostID: "host"},
		{Label: "敏感参数已隐藏", Status: "failed"},
	}
	got := latestFailedStarts(jobs)
	if len(got) != 1 || got[0].VMName != "db" || got[0].HostName != "compute" {
		t.Fatalf("starts = %#v", got)
	}
}

func TestJobsPageRendersFailedStarts(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "jobs", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Jobs": []store.Job{}, "Stats": store.QueueStats{}, "Page": 1, "HasNext": false,
		"FailureGroups": []failureCategory{}, "FailureTargets": []failureTarget{},
		"FailedStarts": []failedStart{{HostID: "host", HostName: "compute", VMName: "db"}},
	}}
	var rendered strings.Builder
	if err := templates["jobs"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"启动仍失败", "db", "compute", "不重试", "/hosts/host/vms/db"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
