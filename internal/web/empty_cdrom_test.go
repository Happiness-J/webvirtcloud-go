package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestEmptyCDROMRequiresAnEmptyDrive(t *testing.T) {
	if emptyCDROM(nil) {
		t.Fatal("a VM without drives was marked")
	}
	loaded := []libvirt.BlockDevice{{Device: "cdrom", Target: "sda", Source: "/images/install.iso"}}
	if emptyCDROM(loaded) {
		t.Fatal("a loaded drive was marked")
	}
	empty := []libvirt.BlockDevice{{Device: "cdrom", Target: "sda", Source: "-"}}
	if !emptyCDROM(empty) {
		t.Fatal("an empty drive was not marked")
	}
}

func TestVMListRendersEmptyCDROM(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "NoAddress": map[string]bool{}, "NoCurrent": map[string]bool{}, "BootMenu": map[string]bool{}, "Exposed": map[string]bool{},
		"EmptyCDROM": map[string]bool{"host/web": true},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !strings.Contains(body, "光驱是空的") || !strings.Contains(body, "不挂载") {
		t.Fatal("marker missing")
	}
}
