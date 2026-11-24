package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestShutdownLeftoversOnlyWhenPoweredOff(t *testing.T) {
	media := []cdromMedia{{Target: "sda", Image: "install.iso"}}
	devices := []libvirt.AssignedDevice{
		{Kind: "usb", Description: "USB 1234:5678", Source: "1234:5678"},
		{Kind: "block", Description: "/dev/disk/by-id/data", Source: "/dev/disk/by-id/data"},
		{Kind: "pci", Description: "网卡", Source: "0000:01:00.0"},
	}
	if got := shutdownLeftovers("running", media, devices); len(got) != 0 {
		t.Fatalf("running VM listed leftovers %#v", got)
	}
	got := shutdownLeftovers("shut off", media, devices)
	if len(got) != 3 || got[0].Kind != "光驱" || got[0].What != "install.iso" || got[1].Kind != "USB" || got[2].What != "data" {
		t.Fatalf("leftovers = %#v", got)
	}
}

func TestShutdownLeftoversSkipEmptyDrive(t *testing.T) {
	if got := shutdownLeftovers("shut off", []cdromMedia{{Target: "sda", Empty: true}}, nil); len(got) != 0 {
		t.Fatalf("empty drive listed %#v", got)
	}
}

func TestBootGroupRendersLeftovers(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "shut off"}},
		"Boot":   bootInventory{Firmware: "BIOS"}, "CDROMs": []cdromMedia{},
		"Leftovers": []leftoverMount{{Kind: "光驱", Where: "sda", What: "install.iso"}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"关机前挂载残留", "install.iso", "不会弹出"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
