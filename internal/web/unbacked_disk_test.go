package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestDisksOutsideCheckpointsSkipsCoveredAndOptical(t *testing.T) {
	devices := []libvirt.BlockDevice{
		{Device: "disk", Target: "vda", Source: "/images/web.qcow2"},
		{Device: "disk", Target: "vdb", Source: "/images/data.qcow2"},
		{Device: "cdrom", Target: "sda", Source: "-"},
	}
	xmlText := `<domaincheckpoint><disks><disk name="vda"/></disks></domaincheckpoint>`
	got := disksOutsideCheckpoints(devices, []string{xmlText})
	if len(got) != 1 || got[0].Target != "vdb" {
		t.Fatalf("uncovered = %#v", got)
	}
}

func TestVMDetailRendersUnbackedDisks(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"UnbackedDisks": []unbackedDisk{{Target: "vdb", Source: "/images/data.qcow2"}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "unbackeddisks", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"没有进入备份链", "vdb", "不创建检查点"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
