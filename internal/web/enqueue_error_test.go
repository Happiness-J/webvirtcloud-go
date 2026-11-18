package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestVMQuotaReservationSeparatesInternalErrors(t *testing.T) {
	if !errors.Is(store.ErrQuotaExceeded, store.ErrQuotaExceeded) || errors.Is(errors.New("database unavailable"), store.ErrQuotaExceeded) {
		t.Fatal("quota sentinel does not separate internal errors")
	}
}

func TestDefaultVMCreateOptionsPass(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/hosts/host/vms", strings.NewReader("name=web-01&os_profile=generic&size=medium"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if defaultForm(request, "cpu_mode", "host-model") != "host-model" {
		t.Fatal("default CPU mode was not applied")
	}
	if _, err := cpuModelArg(defaultForm(request, "cpu_mode", "host-model"), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := firmwareBoot("", "", "bios", ""); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueQuotaFailureUsesFixedMessage(t *testing.T) {
	err := fmt.Errorf("%w: 每个用户最多 2 个排队或运行中的任务", store.ErrJobQuotaExceeded)
	message, ok := enqueueFailureMessage(err)
	if !ok || strings.Contains(message, "job queue") || strings.Contains(message, "最多") {
		t.Fatalf("message = %q", message)
	}
}
