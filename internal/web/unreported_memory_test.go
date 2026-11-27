package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestUnreportedMemoryKeepsOnlyRunningGuests(t *testing.T) {
	domains := []libvirt.Domain{{Name: "web", State: "running"}, {Name: "db", State: "running"}, {Name: "off", State: "shut off"}}
	stats := []libvirt.DomainResourceStats{{Name: "web", BalloonAvailable: 8, BalloonUnused: 3}}
	got := unreportedMemory(domains, stats)
	if len(got) != 1 || !got["db"] {
		t.Fatalf("unreported = %#v", got)
	}
}

func TestVMListRendersUnreportedMemory(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "NoAddress": map[string]bool{}, "NoCurrent": map[string]bool{}, "BootMenu": map[string]bool{}, "EmptyCDROM": map[string]bool{}, "Exposed": map[string]bool{},
		"NoMemory": map[string]bool{"host/web": true},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !strings.Contains(body, "没有实际占用") || !strings.Contains(body, "不调整气球") {
		t.Fatal("marker missing")
	}
}
