package web

import "testing"

func TestFirmwareBoot(t *testing.T) {
	got, err := firmwareBoot("hd", "off", "uefi", "yes")
	if err != nil || got != "hd,menu=off,uefi,secure=on" {
		t.Fatalf("%s %v", got, err)
	}
	if _, err = firmwareBoot("hd", "off", "bios", "yes"); err == nil {
		t.Fatal("secure boot without uefi")
	}
}
