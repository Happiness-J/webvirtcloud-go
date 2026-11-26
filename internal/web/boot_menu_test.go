package web

import (
	"errors"
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestBootMenuOpenSkipsClosedAndFailures(t *testing.T) {
	groups := []vmGroup{{
		Host:    store.Host{ID: "host"},
		Domains: []libvirt.Domain{{Name: "open"}, {Name: "closed"}, {Name: "broken"}},
	}, {
		Host: store.Host{ID: "down"}, Err: "连接失败",
		Domains: []libvirt.Domain{{Name: "hidden"}},
	}}
	xmls := map[string]string{
		"open":   `<domain><os><bootmenu enable="yes"/></os></domain>`,
		"closed": `<domain><os><bootmenu enable="no"/></os></domain>`,
	}
	got := bootMenuOpen(groups, func(_, name string) (string, error) {
		if name == "broken" {
			return "", errors.New("unavailable")
		}
		return xmls[name], nil
	})
	if len(got) != 1 || !got["host/open"] {
		t.Fatalf("open = %#v", got)
	}
}

func TestVMListRendersBootMenu(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vms", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Groups":   []vmGroup{{Host: store.Host{ID: "host", Name: "compute"}, Domains: []libvirt.Domain{{Name: "web", State: "running", Autostart: "enable"}}}},
		"Unbacked": map[string]bool{}, "NoAgent": map[string]bool{}, "NoAddress": map[string]bool{}, "NoCurrent": map[string]bool{}, "Exposed": map[string]bool{},
		"BootMenu": map[string]bool{"host/web": true},
	}}
	var rendered strings.Builder
	if err := templates["vms"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !strings.Contains(body, "启动菜单开着") || !strings.Contains(body, "不关闭") {
		t.Fatal("marker missing")
	}
}
