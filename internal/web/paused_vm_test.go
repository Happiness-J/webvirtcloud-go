package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestPausedVMsKeepsPausedOnly(t *testing.T) {
	groups := []vmGroup{{
		Host: store.Host{ID: "host", Name: "compute"},
		Domains: []libvirt.Domain{
			{Name: "held", State: "paused"},
			{Name: "web", State: "running"},
			{Name: "off", State: "shut off"},
		},
	}, {
		Host: store.Host{ID: "down"}, Err: "连接失败",
		Domains: []libvirt.Domain{{Name: "hidden", State: "paused"}},
	}}
	got := pausedVMs(groups)
	if len(got) != 1 || got[0].Name != "held" || got[0].HostName != "compute" {
		t.Fatalf("paused = %#v", got)
	}
}

func TestVMListRendersPausedVMs(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "Exposed": map[string]bool{},
		"Paused": []pausedVM{{HostID: "host", HostName: "compute", Name: "held"}},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"暂停中的虚拟机", "held", "不会恢复"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
