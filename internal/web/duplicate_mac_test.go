package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestDuplicateMACsKeepsRepeats(t *testing.T) {
	got := duplicateMACs([]macOwner{
		{VM: "web", MAC: "52:54:00:11:22:33"},
		{VM: "db", MAC: "52:54:00:11:22:33"},
		{VM: "web", MAC: "52:54:00:11:22:33"},
		{VM: "once", MAC: "52:54:00:AA:BB:CC"},
		{VM: "bad", MAC: "not-a-mac"},
	})
	if len(got) != 1 || got[0].MAC != "52:54:00:11:22:33" || len(got[0].VMs) != 2 || got[0].VMs[0] != "db" {
		t.Fatalf("dups = %#v", got)
	}
}

func TestNetworkPageRendersDuplicateMAC(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "networks", CSRF: "csrf", User: &store.User{Role: "viewer"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "Networks": nil, "IPPools": []store.IPPool{}, "Allocations": map[string][]store.IPAllocation{},
		"AddressUses": []addressUse{}, "DuplicateMACs": []duplicateMAC{{MAC: "52:54:00:11:22:33", VMs: []string{"db", "web"}}},
	}}
	var rendered strings.Builder
	if err := templates["networks"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"重复 MAC", "52:54:00:11:22:33", "db", "web", "不会修改网卡"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
