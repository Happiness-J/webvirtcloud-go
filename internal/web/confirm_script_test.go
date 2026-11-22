package web

import (
	"os"
	"strings"
	"testing"
)

func TestConfirmScriptChecksFormBeforeDialog(t *testing.T) {
	data, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	validity := strings.Index(script, "form.checkValidity()")
	dialog := strings.Index(script, "window.confirm")
	if validity < 0 || dialog < 0 || validity > dialog {
		t.Fatal("confirmation dialog runs before native form validation")
	}
}
