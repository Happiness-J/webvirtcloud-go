package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestWriteThroughDisksKeepsOnlyThatCache(t *testing.T) {
	xmlText := `<domain><devices>
		<disk type="file" device="disk"><driver cache="writethrough"/><source file="/images/web.qcow2"/><target dev="vda"/></disk>
		<disk type="file" device="disk"><driver cache="none"/><source file="/images/data.qcow2"/><target dev="vdb"/></disk>
		<disk type="file" device="cdrom"><driver cache="writethrough"/><source file="/images/install.iso"/><target dev="sda"/></disk>
	</devices></domain>`
	got := writeThroughDisks(xmlText)
	if len(got) != 1 || got[0].Target != "vda" || got[0].Source != "/images/web.qcow2" {
		t.Fatalf("disks = %#v", got)
	}
	if writeThroughDisks("<not") != nil {
		t.Fatal("invalid XML produced disks")
	}
}

func TestVMDetailRendersWriteThrough(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host", Name: "compute"}, "VM": "web",
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{State: "running"}},
		"Boot":   bootInventory{}, "WriteThrough": []writeThroughDisk{{Target: "vda", Source: "/images/web.qcow2"}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "writethrough", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"写穿透", "vda", "不改缓存"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
