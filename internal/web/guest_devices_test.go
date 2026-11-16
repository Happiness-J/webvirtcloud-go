package web

import (
	"strings"
	"testing"
)

func TestGuestDevices(t *testing.T) {
	state := parseGuestDevices(`<domain><devices><rng model="virtio"></rng><watchdog model="i6300esb" action="shutdown"/></devices></domain>`)
	if !state.RNG || state.Watchdog != "shutdown" {
		t.Fatalf("state=%#v", state)
	}
	args, err := rngArgs("web-01", true)
	if err != nil || strings.Join(args, " ") != "web-01 --add-device --rng /dev/urandom" {
		t.Fatalf("rng=%#v err=%v", args, err)
	}
	args, err = watchdogArgs("web-01", "none", true)
	if err != nil || strings.Join(args, " ") != "web-01 --add-device --watchdog i6300esb,action=none" {
		t.Fatalf("watchdog=%#v err=%v", args, err)
	}
	if _, err = watchdogArgs("web-01", "dump", true); err == nil {
		t.Fatal("unsupported action accepted")
	}
}
