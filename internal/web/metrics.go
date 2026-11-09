package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"webvirtcloud-go/internal/auth"
)

// metrics exposes a deliberately small Prometheus text endpoint. It is
// disabled unless WVC_METRICS_TOKEN is configured, and the token is compared
// in constant time so the endpoint can be scraped without creating a session.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	if !s.metricsAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="wvc-metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	stats, err := s.db.QueueStats(ctx)
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	workers, err := s.db.CountLiveWorkers(ctx, time.Minute)
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	auditOutbox, err := s.db.AuditOutboxStats(ctx)
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP wvc_jobs_total Jobs currently recorded by final or active status.\n")
	fmt.Fprintf(w, "# TYPE wvc_jobs_total gauge\n")
	fmt.Fprintf(w, "wvc_jobs_total{status=\"queued\"} %d\n", stats.Queued)
	fmt.Fprintf(w, "wvc_jobs_total{status=\"running\"} %d\n", stats.Running)
	fmt.Fprintf(w, "wvc_jobs_total{status=\"succeeded\"} %d\n", stats.Succeeded)
	fmt.Fprintf(w, "wvc_jobs_total{status=\"failed\"} %d\n", stats.Failed)
	fmt.Fprintf(w, "wvc_jobs_total{status=\"cancelled\"} %d\n", stats.Cancelled)
	fmt.Fprintf(w, "# HELP wvc_workers_live Workers that published a heartbeat in the last minute.\n")
	fmt.Fprintf(w, "# TYPE wvc_workers_live gauge\n")
	fmt.Fprintf(w, "wvc_workers_live %d\n", workers)
	fmt.Fprintf(w, "# HELP wvc_audit_outbox_total Durable audit events waiting for the primary audit chain.\n")
	fmt.Fprintf(w, "# TYPE wvc_audit_outbox_total gauge\n")
	fmt.Fprintf(w, "wvc_audit_outbox_total %d\n", auditOutbox.Total)
	fmt.Fprintf(w, "# HELP wvc_audit_outbox_due Durable audit events whose retry deadline has elapsed.\n")
	fmt.Fprintf(w, "# TYPE wvc_audit_outbox_due gauge\n")
	fmt.Fprintf(w, "wvc_audit_outbox_due %d\n", auditOutbox.Due)
	fmt.Fprintf(w, "# HELP wvc_audit_outbox_failed Durable audit events that have failed at least once.\n")
	fmt.Fprintf(w, "# TYPE wvc_audit_outbox_failed gauge\n")
	fmt.Fprintf(w, "wvc_audit_outbox_failed %d\n", auditOutbox.Failed)
	fmt.Fprintf(w, "# HELP wvc_console_sessions Active WebSocket console sessions in this process.\n")
	fmt.Fprintf(w, "# TYPE wvc_console_sessions gauge\n")
	fmt.Fprintf(w, "wvc_console_sessions %d\n", s.consoleSessions.Load())
}

func (s *Server) metricsAuthorized(r *http.Request) bool {
	if s.cfg.MetricsToken == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "Bearer "
	if strings.HasPrefix(provided, bearer) {
		provided = strings.TrimPrefix(provided, bearer)
	} else {
		provided = ""
	}
	wantHash := []byte(auth.TokenHash(s.cfg.MetricsToken))
	gotHash := []byte(auth.TokenHash(provided))
	return subtle.ConstantTimeCompare(wantHash, gotHash) == 1
}
