package web

import "testing"

func TestVolumePathMatches(t *testing.T) {
	if !volumePathMatches("/var/lib/libvirt/images/disk.qcow2", "/var/lib/libvirt/images/disk.qcow2") {
		t.Fatal("same path rejected")
	}
	if volumePathMatches("/var/lib/libvirt/images/disk.qcow2", "/var/lib/libvirt/images/other.qcow2") {
		t.Fatal("different path matched")
	}
}

func TestVolumeUsageText(t *testing.T) {
	if got := volumeUsageText(nil); got != "未发现虚拟机引用" {
		t.Fatalf("empty = %s", got)
	}
	got := volumeUsageText([]volumeUsage{{VM: "web", Target: "vda"}})
	if got != "web · vda" {
		t.Fatalf("usage = %s", got)
	}
}
