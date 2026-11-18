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
	if windows.VCPUs != 2 || windows.MemoryGB != 4 || windows.DiskGB != 64 {
		t.Fatalf("windows recommendation = %+v", windows)
	}
	large, err := vmSizeByID("large")
	if err != nil || large.VCPUs != 4 || large.MemoryGB != 8 || large.DiskGB != 80 {
		t.Fatalf("%+v %v", large, err)
	}
}
