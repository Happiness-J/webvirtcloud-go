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
	dialog := strings.Index(script, "showChangePreview")
	if validity < 0 || dialog < 0 || validity > dialog {
		t.Fatal("confirmation preview runs before native form validation")
	}
	if !strings.Contains(script, "连接已隐藏") || !strings.Contains(script, "已隐藏") {
		t.Fatal("preview does not hide connection or secret text")
	}
}
