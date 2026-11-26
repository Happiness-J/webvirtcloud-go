package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestNoAddressMarksOnlyAnsweredGuests(t *testing.T) {
	groups := []vmGroup{{
		Host: store.Host{ID: "host"},
		Domains: []libvirt.Domain{
			{Name: "silent", State: "running"},
			{Name: "empty", State: "running"},
			{Name: "online", State: "running"},
			{Name: "off", State: "shut off"},
		},
	}, {
		Host: store.Host{ID: "down"}, Err: "连接失败",
		Domains: []libvirt.Domain{{Name: "hidden", State: "running"}},
	}}
	answered := map[string]bool{"empty": true, "online": true, "off": true}
	addressed := map[string]bool{"online": true}
	got := noAddressFromProbe(groups,
		func(_, name string) bool { return answered[name] },
		func(_, name string) bool { return addressed[name] })
	if len(got) != 1 || !got["host/empty"] {
		t.Fatalf("no address = %#v", got)
	}
}

func TestVMListRendersNoAddress(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "Exposed": map[string]bool{},
		"NoAddress": map[string]bool{"host/web": true},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !strings.Contains(body, "没有地址") || !strings.Contains(body, "不改网络") {
		t.Fatal("marker missing")
	}
}
