package web

import (
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestCheckMigrationBlocksLocalDisk(t *testing.T) {
	got := checkMigration(true, true, false, false, []libvirt.BlockDevice{{Device: "disk", Source: "/var/lib/libvirt/images/web.qcow2"}})
	if got.OK || len(got.Reasons) != 1 {
		t.Fatalf("check = %#v", got)
	}
}

func TestCheckMigrationAcceptsSharedDisk(t *testing.T) {
	got := checkMigration(true, true, false, false, []libvirt.BlockDevice{{Device: "disk", Source: "nfs.example:/export/web.qcow2"}})
	if !got.OK {
		t.Fatalf("check = %#v", got)
	}
}
