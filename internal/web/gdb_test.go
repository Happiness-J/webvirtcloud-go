package web

import "testing"

func TestGDB(t *testing.T) {
	if gdbCommand(1234) != "-gdb tcp:127.0.0.1:1234" {
		t.Fatal("command mismatch")
	}
	got := parseGDB(`<domain><commandline xmlns:qemu="http://libvirt.org/schemas/domain/qemu/1.0"><arg value="-gdb tcp:127.0.0.1:1234"/></commandline></domain>`)
	if !got.Enabled || got.Port != 1234 {
		t.Fatalf("stub=%#v", got)
	}
	if _, err := gdbPort("22"); err == nil {
		t.Fatal("privileged port accepted")
	}
}
