package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestAutostartGapFor(t *testing.T) {
	cases := []struct {
		state, flag, label string
		mismatch           bool
	}{
		{"shut off", "yes", "该开未开", true},
		{"running", "disable", "关机后不拉起", true},
		{"paused", "no", "关机后不拉起", true},
		{"running", "enable", "", false},
		{"shut off", "no", "", false},
		{"crashed", "yes", "", false},
	}
	for _, tc := range cases {
		got := autostartGapFor(tc.state, tc.flag)
		if got.Mismatch != tc.mismatch || got.Label != tc.label {
			t.Fatalf("%s/%s = %#v", tc.state, tc.flag, got)
		}
	}
}

func TestVMListRendersAutostartGap(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups": []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{
			{Name: "web", State: "shut off", Autostart: "yes"},
			{Name: "db", State: "running", Autostart: "enable"},
		}}},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if strings.Count(body, "pill warn") != 1 || !strings.Contains(body, ">该开未开<") {
		t.Fatalf("gap marker missing or duplicated")
	}
	if strings.Contains(body, ">关机后不拉起<") {
		t.Fatal("matching VM was marked")
	}
}

func TestOpsRendersAutostartGap(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "ops", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host":         store.Host{ID: "host", Name: "compute", CreatedAt: time.Unix(1, 0)},
		"ResourceRows": []opsVMView{{Name: "once", State: "running", Autostart: "disable", VCPU: "1", MemoryCurrent: "-", MemoryMaximum: "-", CPUTime: "-"}},
		"Tags":         map[string][]string{},
	}}
	var rendered strings.Builder
	if err := templates["ops"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "关机后不拉起") {
		t.Fatal("ops gap missing")
	}
}
