package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestDuplicateDiskMountsKeepsRepeats(t *testing.T) {
	got := duplicateDiskMounts([]libvirt.BlockDevice{
		{Device: "disk", Target: "vda", Source: "/var/lib/libvirt/images/data.qcow2"},
		{Device: "disk", Target: "vdb", Source: "/var/lib/libvirt/images/data.qcow2"},
		{Device: "disk", Target: "vdc", Source: "/var/lib/libvirt/images/solo.qcow2"},
		{Device: "cdrom", Target: "sda", Source: "/var/lib/libvirt/images/data.qcow2"},
	})
	if len(got) != 1 || got[0].Name != "data.qcow2" || len(got[0].Targets) != 2 || got[0].Targets[0] != "vda" {
		t.Fatalf("duplicates = %#v", got)
	}
}

func TestVMDetailRendersDuplicateDisks(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "shut off"}, BlockList: []libvirt.BlockDevice{{Device: "disk", Target: "vda", Source: "data.qcow2"}}},
		"Boot":   bootInventory{}, "DuplicateDisks": []duplicateDisk{{Name: "data.qcow2", Targets: []string{"vda", "vdb"}}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"重复挂载", "data.qcow2", "vda", "vdb", "不会卸载"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	start := strings.Index(body, "重复挂载")
	end := strings.Index(body[start:], "磁盘容量")
	if strings.Contains(body[start:start+end], "/var/lib") {
		t.Fatal("directory was rendered in the duplicate list")
	}
}
