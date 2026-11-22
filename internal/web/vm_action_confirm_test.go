package web

import (
	"os"
	"strings"
	"testing"
)

func TestStartActionHasNoConfirmText(t *testing.T) {
	for _, action := range vmActions("admin", "shut off", "enable") {
		if action.Name == "start" && action.Confirm != "" {
			t.Fatalf("start confirm = %q", action.Confirm)
		}
	}
}

func TestActionTemplateSkipsEmptyConfirm(t *testing.T) {
	data, err := os.ReadFile("templates/vm_detail.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{{if and (not .Destructive) .Confirm}}data-confirm=`) {
		t.Fatal("template renders confirmation attribute without text")
	}
}
