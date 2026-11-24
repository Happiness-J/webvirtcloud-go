package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestUnusedInstallersSkipsMountedAndTools(t *testing.T) {
	images := []libraryImage{
		{Name: "install.iso", Category: "installer"},
		{Name: "old.iso", Category: "installer"},
		{Name: "virtio-win.iso", Category: "virtio"},
	}
	mounted := []cdromMedia{{Image: "Install.iso"}, {Empty: true, Target: "sdb"}}
	got := unusedInstallers(images, mounted, 0)
	if len(got.Names) != 1 || got.Names[0] != "old.iso" || got.Incomplete {
		t.Fatalf("unused = %#v", got)
	}
}

func TestUnusedInstallersMarksIncomplete(t *testing.T) {
	got := unusedInstallers([]libraryImage{{Name: "old.iso", Category: "installer"}}, nil, 1)
	if !got.Incomplete || len(got.Names) != 1 {
		t.Fatalf("unused = %#v", got)
	}
}

func TestImagePageRendersUnusedMedia(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "images", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Images": []libraryImage{}, "Query": "", "Category": "all",
		"Unused": unusedMedia{Names: []string{"old.iso"}},
	}}
	var rendered strings.Builder
	if err := templates["images"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"没人挂载", "old.iso", "不会删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
