package web

import (
	"errors"
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestNoCurrentSnapshotRequiresSnapshots(t *testing.T) {
	if noCurrentSnapshot(nil, nil) {
		t.Fatal("a VM without snapshots was marked")
	}
	if noCurrentSnapshot([]libvirt.Snapshot{{Name: "base"}}, errors.New("failed")) {
		t.Fatal("a failed lookup was marked")
	}
	if noCurrentSnapshot([]libvirt.Snapshot{{Name: "base", Current: true}}, nil) {
		t.Fatal("a current snapshot was marked")
	}
	if !noCurrentSnapshot([]libvirt.Snapshot{{Name: "base"}, {Name: "next"}}, nil) {
		t.Fatal("snapshots without a current one were not marked")
	}
}

func TestVMListRendersNoCurrentSnapshot(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "NoAddress": map[string]bool{}, "Exposed": map[string]bool{},
		"NoCurrent": map[string]bool{"host/web": true},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !strings.Contains(body, "没有当前快照") || !strings.Contains(body, "不回滚") {
		t.Fatal("marker missing")
	}
}
