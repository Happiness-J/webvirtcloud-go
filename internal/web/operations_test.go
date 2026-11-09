package web

import (
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestParseScaledAmount(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
		ok    bool
	}{
		{input: "1K", want: 1 << 10, ok: true},
		{input: "10G", want: 10 << 30, ok: true},
		{input: "2T", want: 2 << 40, ok: true},
		{input: "", ok: false},
		{input: "0G", ok: false},
		{input: "-1G", ok: false},
		{input: "+1G", ok: false},
		{input: "1.5G", ok: false},
		{input: "1X", ok: false},
	}
	for _, tc := range tests {
		got, err := parseScaledAmount(tc.input)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("parseScaledAmount(%q) = %d, %v; want %d, ok=%v", tc.input, got, err, tc.want, tc.ok)
		}
	}
}

func TestFindDiskDeviceRejectsCDROMAndUnknownTarget(t *testing.T) {
	devices := []libvirt.BlockDevice{
		{Type: "file", Device: "disk", Target: "vda", Source: "/images/vm.qcow2"},
		{Type: "file", Device: "cdrom", Target: "sda", Source: "/images/install.iso"},
	}
	if disk, ok := findDiskDevice(devices, "vda"); !ok || disk.Source != "/images/vm.qcow2" {
		t.Fatalf("find vda = %#v, %v", disk, ok)
	}
	for _, target := range []string{"sda", "vdb"} {
		if _, ok := findDiskDevice(devices, target); ok {
			t.Errorf("findDiskDevice(%q) unexpectedly succeeded", target)
		}
	}
}

func TestValidVolumeFormat(t *testing.T) {
	for _, format := range []string{"raw", "qcow", "qcow2", "qed", "vmdk", "iso"} {
		if !validVolumeFormat(format) {
			t.Errorf("validVolumeFormat(%q) = false", format)
		}
	}
	if validVolumeFormat("parallels") {
		t.Fatal("validVolumeFormat accepted an untested format")
	}
}
