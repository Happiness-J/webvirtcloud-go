package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestConsolesWithoutPasswordSkipsProtectedDisplays(t *testing.T) {
	got := consolesWithoutPassword([]libvirt.GraphicsDisplay{
		{Type: "vnc", Address: "127.0.0.1"},
		{Type: "spice", Address: "127.0.0.1", Password: true},
		{Type: ""},
	})
	if len(got) != 1 || got[0].Protocol != "vnc" || got[0].Address != "127.0.0.1" {
		t.Fatalf("consoles = %#v", got)
	}
}

func TestVMDetailRendersConsoleWithoutPassword(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"OpenConsoles": []consoleWithoutPassword{{Protocol: "vnc", Address: "127.0.0.1"}},
	}}
	var rendered strings.Builder
	if err := templates["vm_detail"].ExecuteTemplate(&rendered, "consolepassword", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"控制台没有密码", "vnc", "127.0.0.1", "不改密码"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
