package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestCDROMMediaListKeepsEmptyDrives(t *testing.T) {
	got := cdromMediaList([]libvirt.BlockDevice{
		{Device: "disk", Target: "vda", Source: "/var/lib/libvirt/images/vm.qcow2"},
		{Device: "cdrom", Target: "sda", Source: "/var/lib/libvirt/images/installer.iso"},
		{Device: "cdrom", Target: "sdb", Source: "-"},
	})
	if len(got) != 2 || got[0].Image != "installer.iso" || got[0].Empty || !got[1].Empty || got[1].Target != "sdb" {
		t.Fatalf("media = %#v", got)
	}
}

func TestCDROMMediaListHidesDirectory(t *testing.T) {
	got := cdromMediaList([]libvirt.BlockDevice{{Device: "cdrom", Target: "sda", Source: "/secret/path/tools.iso"}})
	if len(got) != 1 || got[0].Image != "tools.iso" || strings.Contains(got[0].Image, "/") {
		t.Fatalf("media = %#v", got)
	}
}

func TestBootGroupRendersCDROMStatus(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "running"}, Info: map[string]string{}, BlockList: []libvirt.BlockDevice{{Device: "cdrom", Target: "sda", Source: "install.iso"}}},
		"Boot":   bootInventory{Firmware: "BIOS", SecureBoot: "未启用", TPM: "未配置", BootOrder: "硬盘", BootMenu: "关闭"},
		"CDROMs": cdromMediaList([]libvirt.BlockDevice{{Device: "cdrom", Target: "sda", Source: "install.iso"}}),
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"光驱与安装介质", "sda", "install.iso", "不会弹出"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(body, "library/media") {
		t.Fatalf("media directory was rendered: %s", body[strings.Index(body, "library")-40:strings.Index(body, "library")+40])
	}
}
