package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

type fakeStore struct {
	mu          sync.Mutex
	recorded    []store.Audit
	recordErr   error
	drainCalls  int
	drainResult int
	drainErr    error
}

func (f *fakeStore) AddAuditReliable(ctx context.Context, entry store.Audit) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.recorded = append(f.recorded, entry)
	return f.recordErr
}

func (f *fakeStore) ProcessAuditOutbox(ctx context.Context, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drainCalls++
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return f.drainResult, f.drainErr
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordDetachesFromRequestCancellation(t *testing.T) {
	db := &fakeStore{}
	recorder := New(db, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder.Record(ctx, store.Audit{Action: "auth.login", ActorName: "admin"})

	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.recorded) != 1 || db.recorded[0].Action != "auth.login" {
		t.Fatalf("recorded = %+v", db.recorded)
	}
}

func TestRecordLogsPersistentFailure(t *testing.T) {
	var logs strings.Builder
	db := &fakeStore{recordErr: errors.New("database unavailable")}
	recorder := New(db, slog.New(slog.NewTextHandler(&logs, nil)))
	recorder.Record(context.Background(), store.Audit{Action: "auth.login_failed"})
	if !strings.Contains(logs.String(), "write audit log") || !strings.Contains(logs.String(), "database unavailable") {
		t.Fatalf("log output = %q", logs.String())
	}
}

func TestRunDrainsImmediatelyAndStops(t *testing.T) {
	db := &fakeStore{drainResult: 2}
	recorder := New(db, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Recorder.Run did not stop after context cancellation")
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if db.drainCalls != 1 {
		t.Fatalf("ProcessAuditOutbox calls = %d, want 1", db.drainCalls)
	}
}
