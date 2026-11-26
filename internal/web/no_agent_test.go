package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestNoAgentMarksOnlyRunningFailures(t *testing.T) {
	groups := []vmGroup{{
		Host: store.Host{ID: "host"},
		Domains: []libvirt.Domain{
			{Name: "web", State: "running"},
			{Name: "db", State: "running"},
			{Name: "off", State: "shut off"},
		},
	}}
	failed := map[string]bool{"web": true, "off": true}
	got := noAgentFromProbe(groups, func(_, name string) bool { return failed[name] })
	if !got["host/web"] || got["host/db"] || got["host/off"] {
		t.Fatalf("marks = %#v", got)
	}
}

func TestVMListRendersNoAgent(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{"host/web": true}, "Exposed": map[string]bool{}, "NoAddress": map[string]bool{},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "没有代理") || !strings.Contains(rendered.String(), "不会安装代理") {
		t.Fatal("agent marker missing")
	}
}
