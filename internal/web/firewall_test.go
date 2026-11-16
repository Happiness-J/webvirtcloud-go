package web

import (
	"strings"
	"testing"
)

func TestFirewallArgs(t *testing.T) {
	args, err := firewallArgs("web-01", "52:54:00:11:22:33", "in", "tcp", "22", true)
	if err != nil || !strings.Contains(strings.Join(args, " "), "filterref=clean-traffic,in,tcp,22,antispoof") {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	if _, err = firewallArgs("web-01", "bad", "in", "tcp", "22", false); err == nil {
		t.Fatal("bad MAC accepted")
	}
}
