package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestIdleVMsKeepsShutOffWithoutAutostart(t *testing.T) {
	groups := []vmGroup{{
		Host: store.Host{ID: "host", Name: "compute"},
		Domains: []libvirt.Domain{
			{Name: "forgotten", State: "shut off", Autostart: "disable"},
			{Name: "nightly", State: "shut off", Autostart: "enable"},
			{Name: "web", State: "running", Autostart: "disable"},
		},
	}, {
		Host: store.Host{ID: "down", Name: "unreachable"}, Err: "连接失败",
		Domains: []libvirt.Domain{{Name: "hidden", State: "shut off"}},
	}}
	got := idleVMs(groups)
	if len(got) != 1 || got[0].Name != "forgotten" || got[0].HostName != "compute" {
		t.Fatalf("idle = %#v", got)
	}
}

func TestVMListRendersIdleVMs(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Unbacked": map[string]bool{}, "Groups": []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Idle": []idleVM{{HostID: "host", HostName: "compute", Name: "forgotten"}},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"无人认领", "forgotten", "不会开机", "不会删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
