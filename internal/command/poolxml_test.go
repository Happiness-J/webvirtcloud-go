package command

import "testing"

func TestStoragePoolXML(t *testing.T) {
	xmlText, err := StoragePoolXML("dir", "images", "/var/lib/libvirt/images", "/var/lib/libvirt/images")
	if err != nil || xmlText != "<pool type='dir'><name>images</name><target><path>/var/lib/libvirt/images</path></target></pool>" {
		t.Fatalf("dir=%q err=%v", xmlText, err)
	}
	xmlText, err = StoragePoolXML("logical", "vg", "vgdata", "")
	if err != nil || xmlText != "<pool type='logical'><name>vg</name><source><name>vgdata</name></source></pool>" {
		t.Fatalf("lvm=%q err=%v", xmlText, err)
	}
	if _, err = StoragePoolXML("netfs", "nfs", "../export", "/mnt/nfs"); err == nil {
		t.Fatal("unsafe NFS source accepted")
	}
}

func TestValidateStoragePoolXML(t *testing.T) {
	xmlText, err := StoragePoolXML("iscsi", "san", "192.0.2.10/iqn.2026-11.lab:target", "")
	if err != nil || ValidateStoragePoolXML(xmlText) != nil {
		t.Fatalf("iscsi=%q err=%v", xmlText, err)
	}
	if err = ValidateStoragePoolXML("<pool type='dir'><name>images</name></pool>"); err == nil {
		t.Fatal("incomplete pool accepted")
	}
}
