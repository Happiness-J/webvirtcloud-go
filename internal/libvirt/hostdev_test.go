package libvirt

import "testing"

func TestParseAssignedHostDevices(t *testing.T) {
	xmlText := `<domain><name>db</name><devices>
	  <hostdev mode="subsystem" type="usb" managed="yes"><source><vendor id="0x0781"/><product id="0x5581"/></source></hostdev>
	  <disk type="block" device="lun"><source dev="/dev/disk/by-id/scsi-data"/><target dev="sdb" bus="scsi"/></disk>
	</devices></domain>`
	got := ParseAssignedHostDevices("db", xmlText)
	if len(got) != 2 || got[0].Source != "0781:5581" {
		t.Fatalf("assigned = %#v", got)
	}
	if got[0].Kind != "usb" || got[1].Kind != "block" || got[1].Source != "/dev/disk/by-id/scsi-data" {
		t.Fatalf("assigned = %#v", got)
	}
}

func TestHostDeviceXMLRejectsSystemDisk(t *testing.T) {
	_, err := HostDeviceXML(HostDevice{Kind: "block", Source: "/dev/sda", Assignable: true}, "sdb")
	if err == nil {
		t.Fatal("system disk was accepted")
	}
	xmlText, err := HostDeviceXML(HostDevice{Kind: "usb", Vendor: "0781", Product: "5581", Assignable: true}, "")
	if err != nil || xmlText != "<hostdev mode='subsystem' type='usb' managed='yes'><source><vendor id='0x0781'/><product id='0x5581'/></source></hostdev>" {
		t.Fatalf("usb xml = %q err=%v", xmlText, err)
	}
}

func TestParseNodeDeviceUSB(t *testing.T) {
	xmlText := `<device><name>usb_device_781_5581</name><capability type="usb_device"><vendor id="0x0781" name="SanDisk"/><product id="0x5581" name="Ultra"/></capability></device>`
	got, ok := parseNodeDevice("usb_device_781_5581", "usb_device", xmlText)
	if !ok || got.Vendor != "0781" || got.Product != "5581" || got.Description != "Ultra" {
		t.Fatalf("device = %#v ok=%v", got, ok)
	}
}
