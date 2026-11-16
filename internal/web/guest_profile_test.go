package web

import (
	"strings"
	"testing"
)

func TestGuestProfileArgs(t *testing.T) {
	args, err := guestProfileArgs("web-01", "windows")
	if err != nil || !strings.Contains(strings.Join(args, " "), "hyperv_relaxed=on") || !strings.Contains(strings.Join(args, " "), "offset=localtime") {
		t.Fatalf("windows=%#v err=%v", args, err)
	}
	args, err = guestProfileArgs("web-01", "linux")
	if err != nil || !strings.Contains(strings.Join(args, " "), "offset=utc") {
		t.Fatalf("linux=%#v err=%v", args, err)
	}
	if _, err = guestProfileArgs("web-01", "other"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}
