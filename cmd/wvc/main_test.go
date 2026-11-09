package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunHealthcheck(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "ready", status: http.StatusOK, body: "ready\n"},
		{name: "not ready", status: http.StatusServiceUnavailable, body: "no live job worker\n", wantErr: "HTTP 503: no live job worker"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			t.Setenv("WVC_HEALTHCHECK_URL", server.URL+"/readyz")
			err := runHealthcheck()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("runHealthcheck() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("runHealthcheck() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestRunHealthcheckRejectsInvalidTarget(t *testing.T) {
	for _, target := range []string{
		"not-a-url",
		"ftp://127.0.0.1/readyz",
		"http://user:password@127.0.0.1/readyz",
		"http://127.0.0.1/readyz#fragment",
	} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("WVC_HEALTHCHECK_URL", target)
			if err := runHealthcheck(); err == nil {
				t.Fatalf("runHealthcheck(%q) unexpectedly succeeded", target)
			}
		})
	}
}

func TestWaitForBackground(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if err := waitForBackground(done, time.Second); err != nil {
		t.Fatalf("waitForBackground(closed) = %v", err)
	}

	if err := waitForBackground(make(chan struct{}), time.Millisecond); err == nil {
		t.Fatal("waitForBackground(open) unexpectedly succeeded")
	}
}
