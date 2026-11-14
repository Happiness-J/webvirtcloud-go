package web

import "testing"

func TestSerialPageRejectsUnsafeName(t *testing.T) {
	if err := validateSerialName("ok-vm"); err != nil {
		t.Fatal(err)
	}
	if err := validateSerialName("../vm"); err == nil {
		t.Fatal("unsafe serial name accepted")
	}
}
