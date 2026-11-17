package web

import "testing"

func TestHostPowerCommand(t *testing.T) {
	tool, args, _, err := hostPowerCommand("updates", "")
	if err != nil || tool != "apt-get" || len(args) != 2 || args[0] != "-s" {
		t.Fatalf("%s %#v %v", tool, args, err)
	}
	if _, _, _, err = hostPowerCommand("other", ""); err == nil {
		t.Fatal("unknown action accepted")
	}
}
