package web

import (
	"strings"
	"testing"
)

func TestImageConvertArgs(t *testing.T) {
	args, err := imageConvertArgs("/var/lib/libvirt/images/disk.qcow2", "qcow2", "disk.vmdk", "vmdk")
	if err != nil || strings.Join(args, " ") != "convert -p -f qcow2 -O vmdk /var/lib/libvirt/images/disk.qcow2 /var/lib/libvirt/images/disk.vmdk" {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	if _, err = imageConvertArgs("/var/lib/libvirt/images/disk.qcow2", "qcow2", "../disk.raw", "raw"); err == nil {
		t.Fatal("path escape accepted")
	}
	if detectImageFormat("/images/disk.qcow2") != "qcow2" || detectImageFormat("/images/disk.img") != "raw" {
		t.Fatal("format detection mismatch")
	}
}
