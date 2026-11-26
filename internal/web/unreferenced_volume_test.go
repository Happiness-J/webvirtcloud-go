package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestUnreferencedVolumesSkipsAttachedDisks(t *testing.T) {
	volumes := []libvirt.Volume{
		{Name: "idle.qcow2", Capacity: "20 GB"},
		{Name: "web.qcow2", Capacity: "40 GB"},
		{Name: ""},
	}
	usage := map[string][]volumeUsage{"web.qcow2": {{VM: "web", Target: "vda"}}}
	got := unreferencedVolumes(volumes, usage)
	if len(got) != 1 || got[0].Name != "idle.qcow2" || got[0].Capacity != "20 GB" {
		t.Fatalf("unused = %#v", got)
	}
}

func TestStoragePageRendersUnreferencedVolumes(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "storage", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host", Name: "compute"}, "SelectedPool": "images",
		"Volumes": []libvirt.Volume{}, "VolumeUsage": map[string][]volumeUsage{},
		"SharedVolumes": []sharedVolume{}, "CapacityHints": map[string]poolCapacityHint{},
		"Unreferenced": []unreferencedVolume{{Name: "idle.qcow2", Capacity: "20 GB"}},
	}}
	var rendered strings.Builder
	if err := templates["storage"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"没有虚拟机引用", "idle.qcow2", "20 GB", "不删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
