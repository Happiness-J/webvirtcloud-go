package web

import (
	"os"
	"strings"
	"testing"
)

func TestVMTemplateFieldsStayControlled(t *testing.T) {
	data, err := os.ReadFile("vm_template.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"Hostname", "Username", "Address", "Gateway", "DNS"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, blocked := range []string{"Password", "PrivateKey", "Token"} {
		if strings.Contains(text, blocked) {
			t.Fatalf("template handler contains %s", blocked)
		}
	}
}
