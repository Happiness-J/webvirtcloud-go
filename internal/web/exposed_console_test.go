package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestConsoleExposed(t *testing.T) {
	open := []libvirt.GraphicsDisplay{{Type: "vnc", Address: "0.0.0.0", Password: false}}
	if !consoleExposed("running", open) {
		t.Fatal("open console was not exposed")
	}
	if consoleExposed("shut off", open) {
		t.Fatal("shut-off VM was marked")
	}
	safe := []libvirt.GraphicsDisplay{{Type: "vnc", Address: "127.0.0.1", Password: true}}
	if consoleExposed("running", safe) {
		t.Fatal("loopback console with a password was marked")
	}
}

func TestVMListRendersExposedConsole(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{},
		"Exposed": map[string]bool{"host/web": true}, "NoAddress": map[string]bool{},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if strings.Count(body, "控制台暴露") < 2 || !strings.Contains(body, "修改监听和密码") {
		t.Fatal("marker missing")
	}
	if !strings.Contains(body, `class="status-pills"`) {
		t.Fatal("status markers are not grouped inline")
	}
	if strings.Contains(body, "0.0.0.0") {
		t.Fatal("listener address was rendered")
	}
}
