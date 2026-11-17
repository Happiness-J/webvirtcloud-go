package store

import (
	"context"
	"testing"
)

func TestSchedulePowerValidation(t *testing.T) {
	db := &Store{}
	err := db.SaveVMSchedule(context.TODO(), VMSchedule{Kind: "power", IntervalHours: 1, RetainCount: 1, PowerAction: "reboot"})
	if err == nil {
		t.Fatal("reboot accepted")
	}
}
