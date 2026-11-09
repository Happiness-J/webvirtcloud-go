package config

import (
	"strings"
	"testing"
	"time"
)

var configEnvKeys = []string{
	"WVC_ADDR", "WVC_DATABASE_URL", "WVC_MASTER_KEY_FILE", "WVC_ADMIN_USERNAME",
	"WVC_ADMIN_PASSWORD", "WVC_SESSION_TTL", "WVC_COMMAND_TIMEOUT", "WVC_MAX_COMMAND_TIMEOUT",
	"WVC_MAX_SESSIONS_PER_USER",
	"WVC_SECURE_COOKIES", "WVC_TRUST_PROXY", "WVC_TRUSTED_PROXY_CIDRS",
	"WVC_MAX_OUTPUT_BYTES", "WVC_MAX_ARGS", "WVC_MAX_STDIN_BYTES", "WVC_WORKERS",
	"WVC_MAX_ACTIVE_JOBS_PER_USER", "WVC_MAX_ACTIVE_JOBS",
	"WVC_CONSOLE_TOKEN_TTL", "WVC_MAX_CONSOLE_SESSIONS", "WVC_LOGIN_WINDOW", "WVC_LOGIN_MAX_FAILURES",
	"WVC_LOGIN_IP_MAX_FAILURES",
	"WVC_LOGIN_GLOBAL_MAX_FAILURES", "WVC_ARGON2_CONCURRENCY", "WVC_JOB_RETENTION",
	"WVC_AUDIT_RETENTION", "WVC_ALLOW_INSECURE_VNC", "WVC_LOG_LEVEL", "WVC_LOG_FORMAT",
	"WVC_METRICS_TOKEN", "WVC_CONSOLE_LOOPBACK_ADDRESS", "WVC_AUDIT_VERIFY_TIMEOUT",
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("WVC_DATABASE_URL", "postgres://wvc:test-only@127.0.0.1:5432/wvc?sslmode=disable")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != DefaultSessionTTL || cfg.CommandTimeout != DefaultCommandTimeout || cfg.LogFormat != "text" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.MaxSessionsPerUser != DefaultMaxSessionsPerUser {
		t.Fatalf("MaxSessionsPerUser = %d, want %d", cfg.MaxSessionsPerUser, DefaultMaxSessionsPerUser)
	}
	if cfg.MetricsToken != "" || cfg.AllowInsecureVNC {
		t.Fatal("secure-by-default options are not secure")
	}
	if cfg.ConsoleMaxSessions != DefaultMaxConsoleSessions || cfg.LoginIPMaxFailures != DefaultLoginIPMaxFailures || cfg.LoginGlobalMaxFailures != 200 {
		t.Fatalf("unexpected resource/rate limits: %+v", cfg)
	}
	if cfg.MaxActiveJobsPerUser != DefaultMaxActiveJobsPerUser || cfg.MaxActiveJobs != DefaultMaxActiveJobs {
		t.Fatalf("unexpected job limits: %+v", cfg)
	}
	if cfg.AuditVerifyTimeout != 30*time.Minute || cfg.ConsoleLoopbackAddress != "" {
		t.Fatalf("unexpected audit/console defaults: %+v", cfg)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearConfigEnv(t)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "WVC_DATABASE_URL is required") {
		t.Fatalf("Load() error = %v, want missing WVC_DATABASE_URL", err)
	}
}

func TestLoadValidOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("WVC_DATABASE_URL", "postgres://wvc:test-only@127.0.0.1:5432/wvc?sslmode=disable")
	t.Setenv("WVC_LOG_FORMAT", "json")
	t.Setenv("WVC_LOGIN_WINDOW", "30m")
	t.Setenv("WVC_MAX_CONSOLE_SESSIONS", "96")
	t.Setenv("WVC_MAX_SESSIONS_PER_USER", "12")
	t.Setenv("WVC_LOGIN_IP_MAX_FAILURES", "40")
	t.Setenv("WVC_LOGIN_GLOBAL_MAX_FAILURES", "400")
	t.Setenv("WVC_MAX_ACTIVE_JOBS_PER_USER", "30")
	t.Setenv("WVC_MAX_ACTIVE_JOBS", "300")
	t.Setenv("WVC_ARGON2_CONCURRENCY", "4")
	t.Setenv("WVC_TRUST_PROXY", "true")
	t.Setenv("WVC_TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::1")
	t.Setenv("WVC_METRICS_TOKEN", strings.Repeat("x", 32))
	t.Setenv("WVC_CONSOLE_LOOPBACK_ADDRESS", "host.docker.internal")
	t.Setenv("WVC_AUDIT_VERIFY_TIMEOUT", "45m")
	t.Setenv("WVC_VIRT_INSTALL_BIN", "/opt/virt/bin/virt-install")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogFormat != "json" || cfg.LoginWindow != 30*time.Minute || cfg.Argon2Concurrency != 4 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.ConsoleMaxSessions != 96 || cfg.LoginIPMaxFailures != 40 || cfg.LoginGlobalMaxFailures != 400 {
		t.Fatalf("resource/rate overrides not applied: %+v", cfg)
	}
	if cfg.MaxActiveJobsPerUser != 30 || cfg.MaxActiveJobs != 300 {
		t.Fatalf("job limit overrides not applied: %+v", cfg)
	}
	if cfg.MaxSessionsPerUser != 12 {
		t.Fatalf("MaxSessionsPerUser override = %d", cfg.MaxSessionsPerUser)
	}
	if cfg.ConsoleLoopbackAddress != "host.docker.internal" || cfg.AuditVerifyTimeout != 45*time.Minute {
		t.Fatalf("console/audit overrides not applied: %+v", cfg)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("trusted proxy CIDRs = %v", cfg.TrustedProxyCIDRs)
	}
	if got := cfg.VirtBins["virt-install"]; got != "/opt/virt/bin/virt-install" {
		t.Fatalf("virt-install bin = %q", got)
	}
	if got := cfg.VirtBins["virt-clone"]; got != "virt-clone" {
		t.Fatalf("virt-clone default bin = %q", got)
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	tests := []struct {
		key, value, want string
	}{
		{"WVC_TRUST_PROXY", "sometimes", "boolean"},
		{"WVC_SESSION_TTL", "forever", "duration"},
		{"WVC_WORKERS", "many", "integer"},
		{"WVC_LOG_LEVEL", "verbose", "WVC_LOG_LEVEL"},
		{"WVC_LOG_FORMAT", "xml", "WVC_LOG_FORMAT"},
		{"WVC_TRUSTED_PROXY_CIDRS", "not-a-cidr", "invalid address"},
		{"WVC_ADMIN_USERNAME", "bad user name", "WVC_ADMIN_USERNAME"},
		{"WVC_ADDR", "localhost", "WVC_ADDR"},
		{"WVC_ADDR", ":70000", "WVC_ADDR port"},
		{"WVC_ADDR", "bad host:8080", "WVC_ADDR"},
		{"WVC_METRICS_TOKEN", "short", "WVC_METRICS_TOKEN"},
		{"WVC_METRICS_TOKEN", strings.Repeat("a", 20) + " with-space " + strings.Repeat("b", 10), "WVC_METRICS_TOKEN"},
		{"WVC_METRICS_TOKEN", strings.Repeat("a", 20) + "\t" + strings.Repeat("b", 12), "WVC_METRICS_TOKEN"},
		{"WVC_SESSION_TTL", "1m", "between"},
		{"WVC_LOGIN_GLOBAL_MAX_FAILURES", "1", "between"},
		{"WVC_MAX_CONSOLE_SESSIONS", "0", "between"},
		{"WVC_MAX_SESSIONS_PER_USER", "0", "between"},
		{"WVC_LOGIN_IP_MAX_FAILURES", "1", "between"},
		{"WVC_CONSOLE_LOOPBACK_ADDRESS", "host.docker.internal:5900", "CONSOLE_LOOPBACK"},
		{"WVC_AUDIT_VERIFY_TIMEOUT", "10s", "AUDIT_VERIFY_TIMEOUT"},
	}
	for _, tc := range tests {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("WVC_DATABASE_URL", "postgres://wvc:test-only@127.0.0.1:5432/wvc?sslmode=disable")
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
