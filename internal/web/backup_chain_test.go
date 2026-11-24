package web

import (
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestBackupChainReady(t *testing.T) {
	items := []libvirt.Backup{{Name: "base"}, {Name: "next", Parent: "base"}}
	if !backupChainReady(items) {
		t.Fatal("valid chain rejected")
	}
	if backupChainReady([]libvirt.Backup{{Name: "next", Parent: "missing"}}) {
		t.Fatal("missing parent accepted")
	}
}
