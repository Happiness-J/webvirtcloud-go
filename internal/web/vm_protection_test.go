package web

import "testing"

func TestVMHasDeleteProtection(t *testing.T) {
	tags := map[string][]string{"web": {"prod", "protected"}}
	if !vmHasDeleteProtection(tags, "web") {
		t.Fatal("protected tag not detected")
	}
	if vmHasDeleteProtection(tags, "other") {
		t.Fatal("missing VM reported protected")
	}
}
