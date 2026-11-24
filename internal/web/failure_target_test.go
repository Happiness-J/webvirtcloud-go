package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestRepeatedFailureTargetsCountsSameName(t *testing.T) {
	jobs := []store.Job{
		{Status: "failed", Label: "web: start", HostName: "compute"},
		{Status: "failed", Label: "web: shutdown", HostName: "compute"},
		{Status: "failed", Label: "扩容卷 images/data +10G"},
		{Status: "failed", Label: "删除卷 images/data"},
		{Status: "failed", Label: "db: start"},
		{Status: "succeeded", Label: "web: start"},
		{Status: "failed", Label: "virsh [敏感参数已隐藏]"},
	}
	got := repeatedFailureTargets(jobs)
	if len(got) != 2 || got[0].Name != "web" || got[0].Count != 2 || got[0].Host != "compute" || got[1].Name != "images/data" || got[1].Count != 2 {
		t.Fatalf("targets = %#v", got)
	}
}

func TestFailureTargetNameSkipsPlainErrors(t *testing.T) {
	if got := failureTargetName("虚拟机名称无效"); got != "" {
		t.Fatalf("plain error became a target: %q", got)
	}
}

func TestJobsPageRendersRepeatedTargets(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "jobs", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Jobs": []store.Job{}, "Stats": store.QueueStats{}, "Page": 1, "HasNext": false,
		"FailureGroups":  []failureCategory{},
		"FailureTargets": []failureTarget{{Name: "web", Count: 3, Host: "compute"}},
	}}
	var rendered strings.Builder
	if err := templates["jobs"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"同一目标", "web", "3", "不会重试"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
