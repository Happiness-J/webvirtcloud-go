package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestLongQueuedJobsKeepsOldQueuedOnly(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	jobs := []store.Job{
		{ID: "old", Label: "迁移 web", Status: "queued", HostName: "compute", CreatedAt: now.Add(-20 * time.Minute)},
		{ID: "fresh", Label: "开机", Status: "queued", CreatedAt: now.Add(-5 * time.Minute)},
		{ID: "run", Label: "关机", Status: "running", CreatedAt: now.Add(-time.Hour)},
		{ID: "hour", Label: "克隆", Status: "queued", CreatedAt: now.Add(-2 * time.Hour)},
	}
	got := longQueuedJobs(jobs, now)
	if len(got) != 2 || got[0].ID != "old" || got[0].Wait != "已等 15 分钟以上" || got[1].Wait != "已等 1 小时以上" {
		t.Fatalf("queued = %#v", got)
	}
}

func TestJobsPageRendersLongQueue(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "jobs", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Jobs": []store.Job{}, "Stats": store.QueueStats{}, "Page": 1, "HasNext": false,
		"FailureGroups": []failureCategory{}, "FailureTargets": []failureTarget{},
		"LongQueued": []longQueuedJob{{ID: "job-1", Label: "迁移 web", Host: "compute", Wait: "已等 15 分钟以上"}},
	}}
	var rendered strings.Builder
	if err := templates["jobs"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"排队过久", "迁移 web", "已等 15 分钟以上", "不会取消"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
