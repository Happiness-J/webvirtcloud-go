package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestSharedVolumePathsKeepsMultiVMOnly(t *testing.T) {
	volumes := []libvirt.Volume{
		{Name: "shared.qcow2", Path: "/var/lib/libvirt/images/shared.qcow2"},
		{Name: "solo.qcow2", Path: "/var/lib/libvirt/images/solo.qcow2"},
	}
	usage := map[string][]volumeUsage{
		"shared.qcow2": {{VM: "web", Target: "vda"}, {VM: "web", Target: "vdb"}, {VM: "db", Target: "vda"}},
		"solo.qcow2":   {{VM: "app", Target: "vda"}},
	}
	got := sharedVolumePaths(volumes, usage)
	if len(got) != 1 || got[0].Name != "shared.qcow2" || got[0].Path != "shared.qcow2" || len(got[0].VMs) != 2 {
		t.Fatalf("shared = %#v", got)
	}
}

func TestStoragePageRendersSharedPaths(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	volumes := []libvirt.Volume{{Name: "shared.qcow2", Pool: "images", Capacity: "1.0 GB"}}
	page := pageData{Title: "storage", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "Pools": []libvirt.Pool{{Name: "images", State: "running", Capacity: "10.0 GB", Available: "9.0 GB"}},
		"SelectedPool": "images", "PoolInfo": []kvRow{}, "Volumes": volumes,
		"VolumeUsage":   map[string][]volumeUsage{},
		"CapacityHints": poolCapacityHints([]libvirt.Pool{{Name: "images", Capacity: "10.0 GB"}}, volumes),
		"SharedVolumes": []sharedVolume{{Name: "shared.qcow2", Path: "shared.qcow2", VMs: []string{"web", "db"}}},
	}}
	var rendered strings.Builder
	if err := templates["storage"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"同一路径被多台虚拟机使用", "web", "db", "不会解除引用"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	start := strings.Index(body, "同一路径被多台虚拟机使用")
	end := strings.Index(body[start:], "</section>")
	section := body[start : start+end]
	if strings.Contains(section, "/var/lib") {
		t.Fatal("directory was rendered in the shared list")
	}
}
