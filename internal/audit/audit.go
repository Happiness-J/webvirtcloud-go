package audit

import (
	"context"
	"log/slog"
	"time"

	"webvirtcloud-go/internal/store"
)

// Recorder centralizes audit writes. The primary hash chain is attempted
// synchronously; a transient failure is persisted in the durable outbox and
// retried by Run. An audit outage still does not roll back an already
// completed business action, but the event is no longer silently dropped when
// PostgreSQL recovers.
type Recorder struct {
	store Store
	log   *slog.Logger
}

// Store is the persistence surface required by audit recording and outbox
// draining. Keeping it small makes failure and cancellation behavior testable.
type Store interface {
	AddAuditReliable(context.Context, store.Audit) error
	ProcessAuditOutbox(context.Context, int) (int, error)
}

func New(db Store, log *slog.Logger) *Recorder { return &Recorder{store: db, log: log} }

func (r *Recorder) Record(ctx context.Context, entry store.Audit) {
	// A state-changing response may already have been committed when the client
	// disconnects. Detach from request cancellation so the security event is
	// still written, while retaining a hard upper bound.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.store.AddAuditReliable(auditCtx, entry); err != nil {
		r.log.Error("write audit log", "action", entry.Action, "error", err)
	}
}

// Run drains the durable audit outbox until the process stops.
func (r *Recorder) Run(ctx context.Context) {
	drain := func() {
		drainCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if n, err := r.store.ProcessAuditOutbox(drainCtx, 100); err != nil && ctx.Err() == nil {
			r.log.Error("drain audit outbox", "error", err)
		} else if n > 0 {
			r.log.Info("drained audit outbox", "count", n)
		}
	}
	drain()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drain()
		}
	}
}
