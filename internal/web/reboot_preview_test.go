package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestBuildRebootPreviewSplitsByAutostart(t *testing.T) {
	got := buildRebootPreview([]libvirt.Domain{
		{Name: "web", State: "running", Autostart: "enable"},
		{Name: "once", State: "running", Autostart: "disable"},
		{Name: "nightly", State: "shut off", Autostart: "yes"},
		{Name: "idle", State: "shut off", Autostart: "no"},
	})
	if len(got.WillStart) != 2 || got.WillStart[0].Name != "web" || got.WillStart[1].Name != "nightly" {
		t.Fatalf("will start = %#v", got.WillStart)
	}
	if len(got.StayDown) != 1 || got.StayDown[0].Name != "once" {
		t.Fatalf("stay down = %#v", got.StayDown)
	}
}

func TestHostDetailRendersRebootPreview(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	preview := buildRebootPreview([]libvirt.Domain{{Name: "web", State: "running", Autostart: "disable"}})
	page := pageData{Title: "host", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host": store.Host{ID: "host", Name: "compute", Enabled: true}, "Redacted": "qemu:///system",
		"Jobs": []store.Job{}, "Impact": maintenanceImpact{}, "Reboot": preview,
	}}
	var rendered strings.Builder
	if err := templates["host_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"重启后拉起", "web", "不会回来", "不会重启"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
