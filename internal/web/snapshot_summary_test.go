package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestSummarizeSnapshotsCountsTree(t *testing.T) {
	got := summarizeSnapshots([]libvirt.Snapshot{
		{Name: "base", Depth: 0},
		{Name: "child", Parent: "base", Depth: 1, Current: true},
		{Name: "leaf", Parent: "child", Depth: 2},
	})
	if got.Count != 3 || got.Current != "child" || got.Depth != 2 || len(got.Parents) != 2 {
		t.Fatalf("summary = %#v", got)
	}
	if got.Parents[0].Name != "base" || got.Parents[0].Children != 1 || got.Parents[1].Children != 1 {
		t.Fatalf("parents = %#v", got.Parents)
	}
}

func TestSummarizeSnapshotsEmpty(t *testing.T) {
	got := summarizeSnapshots(nil)
	if got.Count != 0 || got.Current != "" || len(got.Parents) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestSnapshotTreeRendersSummary(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	shots := []libvirt.Snapshot{{Name: "base", Kind: "internal"}, {Name: "child", Parent: "base", Depth: 1, Current: true, Kind: "external"}}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail":    libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "shut off"}, Snapshots: shots},
		"Boot":      bootInventory{},
		"Snapshots": summarizeSnapshots(shots),
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"快照摘要", "共 2 份", "child", "base", "不会删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
