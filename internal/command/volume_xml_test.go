package command

import "testing"

func TestVolumeXMLDeclaresPermissions(t *testing.T) {
	value, err := VolumeXML("nightly1122fix", "qcow2", 1048576, 0o640, 0, 64055)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateVolumeXML(value, "nightly1122fix", "qcow2", 1048576, 0o640, 0, 64055); err != nil {
		t.Fatal(err)
	}
	if _, err := VolumeXML("../disk", "qcow2", 1048576, 0o640, 0, 64055); err == nil {
		t.Fatal("invalid volume name accepted")
	}
	if err := ValidateVolumeXML(value, "nightly1122fix", "qcow2", 1048576, 0o600, 0, 64055); err == nil {
		t.Fatal("mismatched permissions accepted")
	}
}
