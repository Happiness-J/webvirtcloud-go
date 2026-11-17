package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
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
	s.writeInventoryMetrics(w, ctx)
}

func (s *Server) writeInventoryMetrics(w http.ResponseWriter, ctx context.Context) {
	if s.db == nil || s.libvirt == nil {
		return
	}
	hosts, err := s.db.ListHosts(ctx)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "# HELP wvc_host_cpu_percent Host CPU utilization reported by libvirt.\n# TYPE wvc_host_cpu_percent gauge\n")
	fmt.Fprintf(w, "# HELP wvc_host_memory_bytes Host memory reported by libvirt.\n# TYPE wvc_host_memory_bytes gauge\n")
	fmt.Fprintf(w, "# HELP wvc_vm_cpu_seconds_total Cumulative virtual CPU time.\n# TYPE wvc_vm_cpu_seconds_total counter\n")
	fmt.Fprintf(w, "# HELP wvc_vm_memory_bytes Virtual machine memory reported by the balloon driver.\n# TYPE wvc_vm_memory_bytes gauge\n")
	fmt.Fprintf(w, "# HELP wvc_vm_block_bytes Virtual machine block capacity, allocation, and transferred bytes.\n# TYPE wvc_vm_block_bytes gauge\n")
	for _, host := range hosts {
		if !host.Enabled {
			continue
		}
		hostLabel := promLabel(host.Name)
		stats := s.libvirt.HostResourceStats(ctx, host.ID)
		if value, ok := parseMetricNumber(stats.CPUStats["usage"]); ok {
			fmt.Fprintf(w, "wvc_host_cpu_percent{host=%s} %g\n", hostLabel, value)
		}
		if total, ok := parseMetricNumber(stats.MemoryStats["total"]); ok {
			fmt.Fprintf(w, "wvc_host_memory_bytes{host=%s,kind=\"total\"} %g\n", hostLabel, total*1024)
		}
		if free, ok := parseMetricNumber(stats.MemoryStats["free"]); ok {
			fmt.Fprintf(w, "wvc_host_memory_bytes{host=%s,kind=\"free\"} %g\n", hostLabel, free*1024)
		}
		domains, err := s.libvirt.DomainStats(ctx, host.ID)
		if err != nil {
			continue
		}
		for _, domain := range domains {
			vmLabel := promLabel(domain.Name)
			fmt.Fprintf(w, "wvc_vm_cpu_seconds_total{host=%s,vm=%s} %g\n", hostLabel, vmLabel, float64(domain.CPUTimeNanos)/1e9)
			fmt.Fprintf(w, "wvc_vm_memory_bytes{host=%s,vm=%s,kind=\"current\"} %d\n", hostLabel, vmLabel, domain.BalloonCurrent*1024)
			fmt.Fprintf(w, "wvc_vm_memory_bytes{host=%s,vm=%s,kind=\"maximum\"} %d\n", hostLabel, vmLabel, domain.BalloonMaximum*1024)
			fmt.Fprintf(w, "wvc_vm_block_bytes{host=%s,vm=%s,kind=\"capacity\"} %d\n", hostLabel, vmLabel, domain.BlockCapacity)
			fmt.Fprintf(w, "wvc_vm_block_bytes{host=%s,vm=%s,kind=\"read\"} %d\n", hostLabel, vmLabel, domain.BlockReadBytes)
			fmt.Fprintf(w, "wvc_vm_block_bytes{host=%s,vm=%s,kind=\"write\"} %d\n", hostLabel, vmLabel, domain.BlockWriteBytes)
		}
	}
}

func promLabel(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", ``).Replace(value) + `"`
}

func parseMetricNumber(value string) (float64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, false
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	return number, err == nil
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

func (s *Server) monitoringPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("id"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	s.render(w, r, "monitoring", pageData{Title: "监控对接", Nav: "hosts", Data: map[string]any{"Host": host, "Enabled": s.cfg.MetricsToken != ""}})
}
