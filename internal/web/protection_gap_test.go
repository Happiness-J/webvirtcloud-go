package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestProtectionMismatch(t *testing.T) {
	if got := protectionMismatch(true, false); got.Label != "只有防删锁" {
		t.Fatalf("lock only = %#v", got)
	}
	if got := protectionMismatch(false, true); got.Label != "只有高可用" {
		t.Fatalf("ha only = %#v", got)
	}
	if got := protectionMismatch(true, true); got.Label != "" {
		t.Fatalf("both marked %#v", got)
	}
	if got := protectionMismatch(false, false); got.Label != "" {
		t.Fatalf("neither marked %#v", got)
	}
}

func TestVMDetailRendersProtectionGap(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "shut off"}},
		"Boot":   bootInventory{}, "DeleteProtected": true,
		"ProtectionGap": protectionMismatch(true, false),
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"只有防删锁", "不会自动修改"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestClusterRendersProtectionGap(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "cluster", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Policy": store.ClusterPolicy{Policy: "balanced", FailAfter: 3},
		"Hosts":  []store.Host{}, "Health": map[string]store.HostHealth{},
		"Protected":      []store.HAVM{{HostID: "host", VMName: "web", Protected: true, UpdatedAt: time.Unix(1, 0)}},
		"Suggestion":     struct{ HostName, Reason, HostID string }{},
		"Trends":         map[string]hostCapacityTrend{},
		"ProtectionGaps": map[string]string{"host/web": "只有高可用"},
	}}
	var rendered strings.Builder
	if err := templates["cluster"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "只有高可用") {
		t.Fatal("cluster gap missing")
	}
}
