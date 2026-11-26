package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestDiskBusMismatchesSkipsVirtioAndOptical(t *testing.T) {
	xmlText := `<domain><devices>
		<disk device="disk"><target dev="vda" bus="virtio"/></disk>
		<disk device="disk"><target dev="sda" bus="sata"/></disk>
		<disk device="cdrom"><target dev="sdb" bus="sata"/></disk>
	</devices></domain>`
	got := diskBusMismatches(xmlText)
	if len(got) != 1 || got[0].Target != "sda" || got[0].Bus != "sata" || got[0].Want != "virtio" {
		t.Fatalf("mismatches = %#v", got)
	}
}

func TestVMDetailRendersDiskBus(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"DiskBus": []diskBusMismatch{{Target: "sda", Bus: "sata", Want: "virtio"}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "diskbus", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"总线不一致", "sda", "sata", "virtio", "不改总线"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
