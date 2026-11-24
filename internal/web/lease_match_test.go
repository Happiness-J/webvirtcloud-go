package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestMatchAddressUsesJoinsByAddress(t *testing.T) {
	leases := "Expiry Time   MAC address         Protocol  IP address     Hostname\n" +
		"1700000000    52:54:00:11:22:33   ipv4      192.0.2.10/24  web\n" +
		"1700000001    52:54:00:44:55:66   ipv4      192.0.2.11/24  -\n"
	pools := []store.IPPool{{ID: "p1", Name: "lan"}}
	alloc := map[string][]store.IPAllocation{
		"p1": {
			{Address: "192.0.2.10", Owner: "web"},
			{Address: "192.0.2.20", Owner: "db"},
		},
	}
	got := matchAddressUses(leases, pools, alloc)
	if len(got) != 3 {
		t.Fatalf("rows = %#v", got)
	}
	if got[0].Kind != "两边都有" || got[0].Owner != "web" || got[0].Lease != "web" || got[0].Pool != "lan" {
		t.Fatalf("joined = %#v", got[0])
	}
	if got[1].Kind != "只有租约" || got[1].Address != "192.0.2.11" {
		t.Fatalf("lease only = %#v", got[1])
	}
	if got[2].Kind != "只有地址池" || got[2].Owner != "db" {
		t.Fatalf("pool only = %#v", got[2])
	}
}

func TestParseLeaseAddressesSkipsHeader(t *testing.T) {
	got := parseLeaseAddresses("IP address\n192.0.2.10\nnot an address\n")
	if len(got) != 1 || got[0].Address != "192.0.2.10" {
		t.Fatalf("leases = %#v", got)
	}
}

func TestNetworksPageRendersAddressMatch(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	rows := []addressUse{{Address: "192.0.2.10", Lease: "web", Pool: "lan", Owner: "web", Kind: "两边都有"}}
	page := pageData{Title: "networks", CSRF: "csrf", User: &store.User{Role: "viewer"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "Networks": nil, "IPPools": []store.IPPool{}, "Allocations": map[string][]store.IPAllocation{},
		"AddressUses": rows,
	}}
	var rendered strings.Builder
	if err := templates["networks"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"租约与地址池", "两边都有", "192.0.2.10", "不会释放"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
