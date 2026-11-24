package web

import (
	"testing"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestBuildBackupReport(t *testing.T) {
	report := buildBackupReport([]libvirt.Backup{{Name: "base"}, {Name: "next", Parent: "base"}}, []store.BackupDrill{{Result: "passed", TargetKind: "local", Checksum: "0123456789abcdef"}}, store.BackupPolicy{TargetKind: "nfs", TargetRef: "nas:/export"})
	if report.Chain != "检查点链完整" || report.Checkpoint != "next，父检查点 base" || report.Target != "nfs · nas:/export" {
		t.Fatalf("report = %#v", report)
	}
}
