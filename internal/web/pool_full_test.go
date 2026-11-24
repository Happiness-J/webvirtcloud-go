package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestNearlyFullPoolsUsesOneTenth(t *testing.T) {
	pools := []libvirt.Pool{
		{Name: "images", Capacity: "10.0 GB", Available: "0.5 GB"},
		{Name: "roomy", Capacity: "10.0 GB", Available: "2.0 GB"},
		{Name: "unknown", Capacity: "", Available: ""},
	}
	got := nearlyFullPools("host", "compute", pools)
	if len(got) != 1 || got[0].Name != "images" || got[0].Free != "0.5 GB" || got[0].HostName != "compute" {
		t.Fatalf("full = %#v", got)
	}
}

func TestDashboardRendersNearlyFull(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "dash", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Hosts":     []store.Host{{ID: "host", Name: "compute", Enabled: true}},
		"FullPools": []nearlyFullPool{{HostID: "host", HostName: "compute", Name: "images", Free: "0.5 GB"}},
		"Stats":     store.QueueStats{}, "Jobs": []store.Job{},
	}}
	var rendered strings.Builder
	if err := templates["dashboard"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"即将写满", "images", "0.5 GB", "不会迁移"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
