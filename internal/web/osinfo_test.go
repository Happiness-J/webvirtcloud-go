package web

import "testing"

func TestOSProfilesRecommendDevices(t *testing.T) {
	windows, err := osProfileByID("windows")
	if err != nil || windows.DiskBus != "sata" || windows.NIC != "e1000e" || windows.Firmware != "uefi" {
		t.Fatalf("%+v %v", windows, err)
	}
	if _, err = osProfileByID("unknown"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}
