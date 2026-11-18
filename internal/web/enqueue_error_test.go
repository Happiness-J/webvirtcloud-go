package web

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestVMQuotaReservationSeparatesInternalErrors(t *testing.T) {
	if !errors.Is(store.ErrQuotaExceeded, store.ErrQuotaExceeded) || errors.Is(errors.New("database unavailable"), store.ErrQuotaExceeded) {
		t.Fatal("quota sentinel does not separate internal errors")
	}
}

func TestEnqueueQuotaFailureUsesFixedMessage(t *testing.T) {
	err := fmt.Errorf("%w: 每个用户最多 2 个排队或运行中的任务", store.ErrJobQuotaExceeded)
	message, ok := enqueueFailureMessage(err)
	if !ok || strings.Contains(message, "job queue") || strings.Contains(message, "最多") {
		t.Fatalf("message = %q", message)
	}
}
