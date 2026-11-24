package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestBackupFreshness(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	none := backupFreshness(nil, now)
	if none.Status != "none" || none.Label != "没有检查点" {
		t.Fatalf("none = %#v", none)
	}
	fresh := backupFreshness([]libvirt.Backup{{CreatedAt: "2026-11-20 00:00:00"}}, now)
	if fresh.Status != "fresh" {
		t.Fatalf("fresh = %#v", fresh)
	}
	stale := backupFreshness([]libvirt.Backup{{CreatedAt: "2026-10-01 00:00:00"}, {CreatedAt: "2026-11-01"}}, now)
	if stale.Status != "stale" || !strings.Contains(stale.When, "2026-11-01") {
		t.Fatalf("stale = %#v", stale)
	}
}

func TestVMDetailRendersBackupAge(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "shut off"}},
		"Boot":   bootInventory{}, "BackupAge": backupAge{Status: "none", Label: "没有检查点"},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"备份新旧", "没有检查点", "不会创建备份"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
