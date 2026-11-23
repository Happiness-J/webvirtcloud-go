package libvirt

import "testing"

func TestParseVolumeXML(t *testing.T) {
	value := `<volume type='file'><name>disk.qcow2</name><capacity unit='bytes'>67108864</capacity><target path='/var/lib/libvirt/images/disk.qcow2'><format type='qcow2'/></target></volume>`
	metadata, err := ParseVolumeXML(value)
	if err != nil {
		t.Fatalf("ParseVolumeXML() error = %v", err)
	}
	if metadata.Name != "disk.qcow2" || metadata.Capacity != 67108864 || metadata.Format != "qcow2" {
		t.Fatalf("ParseVolumeXML() = %+v", metadata)
	}
	if _, err := ParseVolumeXML("<volume><name>disk.qcow2</name></volume>"); err == nil {
		t.Fatal("ParseVolumeXML() accepted missing metadata")
	}
}
