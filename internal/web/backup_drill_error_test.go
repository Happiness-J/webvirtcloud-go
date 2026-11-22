package web

import (
	"errors"
	"testing"
)

func TestBackupDrillErrorKeepsValidationMessage(t *testing.T) {
	err := errors.New("本地目标不需要地址")
	if got := backupDrillError(err); got != err.Error() {
		t.Fatalf("backupDrillError() = %q", got)
	}
}

func TestBackupDrillErrorHidesUnexpectedMessage(t *testing.T) {
	if got := backupDrillError(errors.New("database unavailable")); got != "无法保存恢复演练" {
		t.Fatalf("backupDrillError() = %q", got)
	}
}
