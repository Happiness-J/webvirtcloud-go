package command

import "testing"

func TestValidateOVAArgs(t *testing.T) {
	ok := []string{"-i", "ova", "/var/lib/libvirt/images/guest.ova", "-o", "local", "-os", "/var/lib/libvirt/images"}
	if err := validateOVAArgs(ok); err != nil {
		t.Fatal(err)
	}
	bad := []string{"-i", "ova", "/tmp/../guest.ova", "-o", "local", "-os", "/var/lib/libvirt/images"}
	if err := validateOVAArgs(bad); err == nil {
		t.Fatal("unsafe ova accepted")
	}
}
