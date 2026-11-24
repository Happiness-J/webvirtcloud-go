package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestBuildMaintenanceImpactSeparatesConcerns(t *testing.T) {
	domains := []libvirt.Domain{
		{Name: "web", State: "running", Autostart: "yes"},
		{Name: "batch", State: "shut off", Autostart: "yes"},
		{Name: "once", State: "paused", Autostart: "no"},
		{Name: "idle", State: "shut off", Autostart: "no"},
	}
	jobs := []store.Job{
		{ID: "j1", Label: "迁移", Status: "running"},
		{ID: "j2", Label: "已完成", Status: "succeeded"},
		{ID: "j3", Label: "排队", Status: "queued"},
	}
	got := buildMaintenanceImpact(domains, jobs, false)
	if len(got.Running) != 2 || got.Running[0].Name != "web" || got.Running[1].Name != "once" {
		t.Fatalf("running = %#v", got.Running)
	}
	if len(got.Autostart) != 2 || len(got.OpenJobs) != 2 || got.OpenJobs[0].ID != "j1" {
		t.Fatalf("impact = %#v", got)
	}
}

func TestBuildMaintenanceImpactKeepsUnavailableSeparate(t *testing.T) {
	got := buildMaintenanceImpact(nil, []store.Job{{ID: "j", Status: "queued"}}, true)
	if !got.Unavailable || len(got.OpenJobs) != 1 || len(got.Running) != 0 {
		t.Fatalf("impact = %#v", got)
	}
}

func TestHostDetailRendersMaintenanceImpact(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	impact := buildMaintenanceImpact([]libvirt.Domain{{Name: "web", State: "running", Autostart: "yes"}}, []store.Job{{ID: "job-1", Label: "迁移 web", Status: "running"}}, false)
	page := pageData{Title: "host", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host": store.Host{ID: "host", Name: "compute", Enabled: true}, "Redacted": "qemu:///system",
		"Jobs": []store.Job{}, "Impact": impact,
	}}
	var rendered strings.Builder
	if err := templates["host_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"维护前影响", "web", "迁移 web", "不会自动迁移"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
