package web

import (
	"fmt"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

type backupReport struct {
	Chain      string
	Checkpoint string
	Drill      string
	Target     string
}

func buildBackupReport(backups []libvirt.Backup, drills []store.BackupDrill, policy store.BackupPolicy) backupReport {
	report := backupReport{Chain: "检查点链不完整", Checkpoint: "没有检查点", Drill: "没有恢复演练", Target: "未配置目标"}
	if backupChainReady(backups) {
		report.Chain = "检查点链完整"
	}
	if len(backups) > 0 {
		last := backups[len(backups)-1]
		parent := last.Parent
		if parent == "" {
			parent = "链起点"
		}
		report.Checkpoint = fmt.Sprintf("%s，父检查点 %s", last.Name, parent)
	}
	if len(drills) > 0 {
		drill := drills[0]
		report.Drill = fmt.Sprintf("%s · %s · %s", drill.Result, drill.TargetKind, shortChecksum(drill.Checksum))
	}
	if policy.TargetKind != "" {
		report.Target = policy.TargetKind
		if policy.TargetRef != "" {
			report.Target += " · " + policy.TargetRef
		}
	}
	return report
}

func shortChecksum(value string) string {
	if len(value) < 12 {
		return value
	}
	return value[:12]
}
