package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestDeepSnapshotChainsKeepsFiveLevels(t *testing.T) {
	domains := []libvirt.Domain{{Name: "web"}, {Name: "db"}, {Name: "missing"}}
	snapshots := map[string][]libvirt.Snapshot{
		"web": {{Name: "a", Depth: 5}},
		"db":  {{Name: "a", Depth: 4}},
	}
	got := deepSnapshotChains("host", "compute", domains, snapshots)
	if len(got) != 1 || got[0].Name != "web" || got[0].Depth != 5 {
		t.Fatalf("deep = %#v", got)
	}
}

func TestVMListRendersDeepSnapshots(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups": []vmGroup{}, "DeepSnapshots": []deepSnapshot{{HostID: "host", HostName: "compute", Name: "web", Depth: 5}},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"快照链过深", "web", "5 层", "不合并", "不删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
