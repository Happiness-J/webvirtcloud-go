package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,64}$`)

const (
	DefaultAddr                 = ":8080"
	DefaultSessionTTL           = 12 * time.Hour
	DefaultMaxSessionsPerUser   = 20
	DefaultCommandTimeout       = 15 * time.Minute
	DefaultMaxTimeout           = time.Hour
	DefaultWorkers              = 2
	DefaultMaxConsoleSessions   = 64
	DefaultLoginIPMaxFailures   = 25
	DefaultMaxActiveJobsPerUser = 20
	DefaultMaxActiveJobs        = 200
	DefaultRemotePATH           = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

// VirtToolNames is the complete virt/libguestfs command-line family exposed
// by the command center. Some distributions package only a subset; the UI
// reports those as missing instead of hiding their documentation.
var VirtToolNames = []string{
	"virt-admin", "virt-host-validate", "virt-qemu-run", "virt-login-shell",
	"virt-xml-validate", "virt-pki-validate", "virt-sanlock-cleanup",
	"virt-install", "virt-clone", "virt-convert", "virt-xml",
	"guestfish", "guestmount", "guestunmount",
	"libguestfs-test-tool", "libguestfs-make-fixed-appliance",
	"virt-alignment-scan", "virt-builder", "virt-builder-repository",
	"virt-cat", "virt-copy-in", "virt-copy-out", "virt-customize", "virt-df",
	"virt-dib", "virt-diff", "virt-edit", "virt-filesystems", "virt-format",
	"virt-get-kernel", "virt-inspector", "virt-list-filesystems",
	"virt-list-partitions", "virt-log", "virt-ls", "virt-make-fs",
	"virt-p2v", "virt-p2v-make-disk", "virt-p2v-make-kickstart", "virt-rescue",
	"virt-resize", "virt-sparsify", "virt-sysprep", "virt-tail", "virt-tar",
	"virt-tar-in", "virt-tar-out", "virt-v2v", "virt-v2v-in-place",
	"virt-v2v-inspector", "virt-v2v-open", "virt-win-reg",
	"virt-what", "virt-top",
}

func virtBinEnv(name string) string {
	return "WVC_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_BIN"
}

// Config contains process-wide settings. It is intentionally environment
// driven so the same binary can run under systemd, Docker or a small VM.
type Config struct {
	Addr               string
	DatabaseURL        string
	SessionTTL         time.Duration
	MaxSessionsPerUser int
	CommandTimeout     time.Duration
	MaxTimeout         time.Duration
	SecureCookies      bool
	TrustProxy         bool
	TrustedProxyCIDRs  []string
	MasterKeyFile      string
	// ImageDir is used by diagnostics to verify that local QEMU image tools
	// can write the directory referenced by local libvirt VM definitions.
	ImageDir             string
	AdminUsername        string
	AdminPassword        string
	VirshBin             string
	QemuImgBin           string
	QemuIOBin            string
	QemuNbdBin           string
	QemuStorageDaemonBin string
	QemuPRHelperBin      string
	QemuTraceStapBin     string
	QemuVMSRHelperBin    string
	QemuVNCBin           string
	SSHBin               string
	// RemotePATH is the fixed PATH used for tools executed over qemu+ssh.
	// Remote binaries are deliberately addressed by canonical command name,
	// not by a path from the panel container.
	RemotePATH string
	// VirtBins maps each virt-* helper to its configured executable. The
	// helpers are optional because a panel can manage libvirt without the
	// libguestfs/virtinst families installed. Missing tools remain visible in
	// the command catalog with a clear availability badge.
	VirtBins       map[string]string
	MaxOutputBytes int
	MaxArgs        int
	MaxStdinBytes  int
	LogLevel       string
	LogFormat      string
	Workers        int
	// MaxActiveJobsPerUser and MaxActiveJobs bound queued+running jobs. A
	// per-host serialization lock prevents conflicting mutations, while these
	// limits prevent a single account or a compromised admin session from
	// filling the queue with work that can never drain.
	MaxActiveJobsPerUser int
	MaxActiveJobs        int
	ConsoleTokenTTL      time.Duration
	ConsoleMaxSessions   int
	LoginWindow          time.Duration
	LoginMaxFailures     int
	// LoginIPMaxFailures limits failures from one source IP across all
	// usernames, so a single client cannot spray many accounts.
	LoginIPMaxFailures int
	// LoginGlobalMaxFailures is an observability threshold for a distributed
	// password spray. It is intentionally not enforced as a hard block: a
	// global counter can be exhausted by an attacker and would then lock every
	// legitimate account out of the panel.
	LoginGlobalMaxFailures int
	Argon2Concurrency      int
	JobRetention           time.Duration
	AuditRetention         time.Duration
	AuditVerifyTimeout     time.Duration
	MetricsToken           string
	// AllowInsecureVNC is intentionally opt-in. VNC itself has no transport
	// encryption, so qemu+tcp/qemu+tls consoles are rejected by default.
	AllowInsecureVNC bool
	// AllowInsecureConsoleContext permits noVNC to initialize when the browser
	// page itself is served over plain HTTP. It is separate from
	// AllowInsecureVNC because a local libvirt console can still be exposed to
	// an untrusted browser context.
	AllowInsecureConsole bool
	// ConsoleLoopbackAddress rewrites VNC endpoints that listen on loopback or
	// all interfaces when the panel itself runs in a container. Leave empty for
	// native deployments. Docker Compose sets this to host.docker.internal.
	ConsoleLoopbackAddress string
	LDAPEnabled            bool
	LDAPURL                string
	LDAPBindDN             string
	LDAPBindPassword       string
	LDAPBaseDN             string
	LDAPUserFilter         string
	LDAPStartTLS           bool
	LDAPInsecureTLS        bool
	LDAPTimeout            time.Duration
	WebhookURL             string
	WebhookToken           string
	WebhookTimeout         time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error

	cfg.Addr = env("WVC_ADDR", DefaultAddr)
	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("WVC_DATABASE_URL"))
	cfg.MasterKeyFile = env("WVC_MASTER_KEY_FILE", "data/master.key")
	cfg.ImageDir = env("WVC_LOCAL_IMAGE_DIR", "/var/lib/libvirt/images")
	cfg.AdminUsername = env("WVC_ADMIN_USERNAME", "admin")
	cfg.AdminPassword = os.Getenv("WVC_ADMIN_PASSWORD")
	cfg.VirshBin = env("WVC_VIRSH_BIN", "virsh")
	cfg.QemuImgBin = env("WVC_QEMU_IMG_BIN", "qemu-img")
	cfg.QemuIOBin = env("WVC_QEMU_IO_BIN", "qemu-io")
	cfg.QemuNbdBin = env("WVC_QEMU_NBD_BIN", "qemu-nbd")
	cfg.QemuStorageDaemonBin = env("WVC_QEMU_STORAGE_DAEMON_BIN", "qemu-storage-daemon")
	cfg.QemuPRHelperBin = env("WVC_QEMU_PR_HELPER_BIN", "qemu-pr-helper")
	cfg.QemuTraceStapBin = env("WVC_QEMU_TRACE_STAP_BIN", "qemu-trace-stap")
	cfg.QemuVMSRHelperBin = env("WVC_QEMU_VMSR_HELPER_BIN", "qemu-vmsr-helper")
	cfg.QemuVNCBin = env("WVC_QEMU_VNC_BIN", "qemu-vnc")
	cfg.SSHBin = env("WVC_SSH_BIN", "ssh")
	cfg.RemotePATH = env("WVC_REMOTE_PATH", DefaultRemotePATH)
	cfg.VirtBins = make(map[string]string, len(VirtToolNames))
	for _, name := range VirtToolNames {
		cfg.VirtBins[name] = env(virtBinEnv(name), name)
	}
	cfg.LogLevel = strings.ToLower(env("WVC_LOG_LEVEL", "info"))
	cfg.LogFormat = strings.ToLower(env("WVC_LOG_FORMAT", "text"))
	cfg.TrustedProxyCIDRs = csvEnv("WVC_TRUSTED_PROXY_CIDRS", []string{"127.0.0.0/8", "::1/128"})
	cfg.MetricsToken = os.Getenv("WVC_METRICS_TOKEN")
	cfg.ConsoleLoopbackAddress = env("WVC_CONSOLE_LOOPBACK_ADDRESS", "")
	cfg.LDAPURL = env("WVC_LDAP_URL", "")
	cfg.LDAPBindDN = env("WVC_LDAP_BIND_DN", "")
	cfg.LDAPBindPassword = os.Getenv("WVC_LDAP_BIND_PASSWORD")
	cfg.LDAPBaseDN = env("WVC_LDAP_BASE_DN", "")
	cfg.LDAPUserFilter = env("WVC_LDAP_USER_FILTER", "(uid=%s)")
	cfg.WebhookURL = env("WVC_WEBHOOK_URL", "")
	cfg.WebhookToken = os.Getenv("WVC_WEBHOOK_TOKEN")

	if cfg.SessionTTL, err = durationEnv("WVC_SESSION_TTL", DefaultSessionTTL); err != nil {
		return Config{}, err
	}
	if cfg.MaxSessionsPerUser, err = intEnv("WVC_MAX_SESSIONS_PER_USER", DefaultMaxSessionsPerUser); err != nil {
		return Config{}, err
	}
	if cfg.CommandTimeout, err = durationEnv("WVC_COMMAND_TIMEOUT", DefaultCommandTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxTimeout, err = durationEnv("WVC_MAX_COMMAND_TIMEOUT", DefaultMaxTimeout); err != nil {
		return Config{}, err
	}
	if cfg.SecureCookies, err = boolEnv("WVC_SECURE_COOKIES", false); err != nil {
		return Config{}, err
	}
	if cfg.TrustProxy, err = boolEnv("WVC_TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.MaxOutputBytes, err = intEnv("WVC_MAX_OUTPUT_BYTES", 1024*1024); err != nil {
		return Config{}, err
	}
	if cfg.MaxArgs, err = intEnv("WVC_MAX_ARGS", 128); err != nil {
		return Config{}, err
	}
	if cfg.MaxStdinBytes, err = intEnv("WVC_MAX_STDIN_BYTES", 1024*1024); err != nil {
		return Config{}, err
	}
	if cfg.Workers, err = intEnv("WVC_WORKERS", DefaultWorkers); err != nil {
		return Config{}, err
	}
	if cfg.MaxActiveJobsPerUser, err = intEnv("WVC_MAX_ACTIVE_JOBS_PER_USER", DefaultMaxActiveJobsPerUser); err != nil {
		return Config{}, err
	}
	if cfg.MaxActiveJobs, err = intEnv("WVC_MAX_ACTIVE_JOBS", DefaultMaxActiveJobs); err != nil {
		return Config{}, err
	}
	if cfg.ConsoleTokenTTL, err = durationEnv("WVC_CONSOLE_TOKEN_TTL", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ConsoleMaxSessions, err = intEnv("WVC_MAX_CONSOLE_SESSIONS", DefaultMaxConsoleSessions); err != nil {
		return Config{}, err
	}
	if cfg.LoginWindow, err = durationEnv("WVC_LOGIN_WINDOW", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.LoginMaxFailures, err = intEnv("WVC_LOGIN_MAX_FAILURES", 5); err != nil {
		return Config{}, err
	}
	if cfg.LoginIPMaxFailures, err = intEnv("WVC_LOGIN_IP_MAX_FAILURES", DefaultLoginIPMaxFailures); err != nil {
		return Config{}, err
	}
	if cfg.LoginGlobalMaxFailures, err = intEnv("WVC_LOGIN_GLOBAL_MAX_FAILURES", 200); err != nil {
		return Config{}, err
	}
	if cfg.Argon2Concurrency, err = intEnv("WVC_ARGON2_CONCURRENCY", 2); err != nil {
		return Config{}, err
	}
	if cfg.JobRetention, err = durationEnv("WVC_JOB_RETENTION", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.AuditRetention, err = durationEnv("WVC_AUDIT_RETENTION", 180*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.AuditVerifyTimeout, err = durationEnv("WVC_AUDIT_VERIFY_TIMEOUT", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.AllowInsecureVNC, err = boolEnv("WVC_ALLOW_INSECURE_VNC", false); err != nil {
		return Config{}, err
	}
	if cfg.AllowInsecureConsole, err = boolEnv("WVC_ALLOW_INSECURE_CONSOLE", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPEnabled, err = boolEnv("WVC_LDAP_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPStartTLS, err = boolEnv("WVC_LDAP_STARTTLS", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPInsecureTLS, err = boolEnv("WVC_LDAP_INSECURE_TLS", false); err != nil {
		return Config{}, err
	}
	if cfg.LDAPTimeout, err = durationEnv("WVC_LDAP_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WebhookTimeout, err = durationEnv("WVC_WEBHOOK_TIMEOUT", 3*time.Second); err != nil {
		return Config{}, err
	}

	if cfg.LDAPEnabled && (cfg.LDAPURL == "" || cfg.LDAPBaseDN == "" || !strings.Contains(cfg.LDAPUserFilter, "%s")) {
		return Config{}, fmt.Errorf("WVC_LDAP_URL、WVC_LDAP_BASE_DN 和包含 %%s 的 WVC_LDAP_USER_FILTER 都是必需的")
	}
	if cfg.WebhookURL != "" {
		parsed, parseErr := url.Parse(cfg.WebhookURL)
		if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return Config{}, fmt.Errorf("WVC_WEBHOOK_URL 必须是 http 或 https 地址")
		}
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, fmt.Errorf("WVC_DATABASE_URL is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return Config{}, fmt.Errorf("WVC_ADDR is required")
	}
	if err := validateListenAddress(cfg.Addr); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.MasterKeyFile) == "" {
		return Config{}, fmt.Errorf("WVC_MASTER_KEY_FILE is required")
	}
	if !filepath.IsAbs(cfg.ImageDir) || strings.ContainsAny(cfg.ImageDir, "\x00\r\n") {
		return Config{}, fmt.Errorf("WVC_LOCAL_IMAGE_DIR must be an absolute path without control characters")
	}
	if strings.TrimSpace(cfg.SSHBin) == "" {
		return Config{}, fmt.Errorf("WVC_SSH_BIN is required")
	}
	if strings.TrimSpace(cfg.RemotePATH) == "" || strings.ContainsAny(cfg.RemotePATH, "\x00\r\n") {
		return Config{}, fmt.Errorf("WVC_REMOTE_PATH must be a non-empty PATH value without control characters")
	}
	if !adminUsernamePattern.MatchString(cfg.AdminUsername) {
		return Config{}, fmt.Errorf("WVC_ADMIN_USERNAME must match %s", adminUsernamePattern.String())
	}
	for _, value := range cfg.TrustedProxyCIDRs {
		if net.ParseIP(value) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return Config{}, fmt.Errorf("WVC_TRUSTED_PROXY_CIDRS contains invalid address %q", value)
		}
	}
	if cfg.SessionTTL < 5*time.Minute || cfg.SessionTTL > 30*24*time.Hour {
		return Config{}, fmt.Errorf("WVC_SESSION_TTL must be between 5m and 720h")
	}
	if cfg.MaxSessionsPerUser < 1 || cfg.MaxSessionsPerUser > 1000 {
		return Config{}, fmt.Errorf("WVC_MAX_SESSIONS_PER_USER must be between 1 and 1000")
	}
	if cfg.CommandTimeout <= 0 || cfg.MaxTimeout < cfg.CommandTimeout || cfg.MaxTimeout > 24*time.Hour {
		return Config{}, fmt.Errorf("command timeout must be positive and <= WVC_MAX_COMMAND_TIMEOUT (max 24h)")
	}
	if cfg.MaxOutputBytes < 4096 || cfg.MaxOutputBytes > 16*1024*1024 {
		return Config{}, fmt.Errorf("WVC_MAX_OUTPUT_BYTES must be between 4096 and 16777216")
	}
	if cfg.MaxArgs < 1 || cfg.MaxArgs > 4096 {
		return Config{}, fmt.Errorf("WVC_MAX_ARGS must be between 1 and 4096")
	}
	if cfg.MaxStdinBytes < 0 || cfg.MaxStdinBytes > 16*1024*1024 {
		return Config{}, fmt.Errorf("WVC_MAX_STDIN_BYTES must be between 0 and 16777216")
	}
	if cfg.Workers < 1 || cfg.Workers > 32 {
		return Config{}, fmt.Errorf("WVC_WORKERS must be between 1 and 32")
	}
	if cfg.MaxActiveJobsPerUser < 1 || cfg.MaxActiveJobsPerUser > 1000 {
		return Config{}, fmt.Errorf("WVC_MAX_ACTIVE_JOBS_PER_USER must be between 1 and 1000")
	}
	if cfg.MaxActiveJobs < cfg.MaxActiveJobsPerUser || cfg.MaxActiveJobs > 10000 {
		return Config{}, fmt.Errorf("WVC_MAX_ACTIVE_JOBS must be between WVC_MAX_ACTIVE_JOBS_PER_USER and 10000")
	}
	if cfg.ConsoleTokenTTL < 15*time.Second || cfg.ConsoleTokenTTL > 10*time.Minute {
		return Config{}, fmt.Errorf("WVC_CONSOLE_TOKEN_TTL must be between 15s and 10m")
	}
	if cfg.ConsoleMaxSessions < 1 || cfg.ConsoleMaxSessions > 1024 {
		return Config{}, fmt.Errorf("WVC_MAX_CONSOLE_SESSIONS must be between 1 and 1024")
	}
	if cfg.LoginWindow < time.Minute || cfg.LoginWindow > 24*time.Hour {
		return Config{}, fmt.Errorf("WVC_LOGIN_WINDOW must be between 1m and 24h")
	}
	if cfg.LoginMaxFailures < 1 || cfg.LoginMaxFailures > 100 {
		return Config{}, fmt.Errorf("WVC_LOGIN_MAX_FAILURES must be between 1 and 100")
	}
	if cfg.LoginIPMaxFailures < cfg.LoginMaxFailures || cfg.LoginIPMaxFailures > 1000 {
		return Config{}, fmt.Errorf("WVC_LOGIN_IP_MAX_FAILURES must be between WVC_LOGIN_MAX_FAILURES and 1000")
	}
	if cfg.LoginGlobalMaxFailures < cfg.LoginIPMaxFailures || cfg.LoginGlobalMaxFailures > 5000 {
		return Config{}, fmt.Errorf("WVC_LOGIN_GLOBAL_MAX_FAILURES must be between WVC_LOGIN_IP_MAX_FAILURES and 5000")
	}
	if cfg.Argon2Concurrency < 1 || cfg.Argon2Concurrency > 32 {
		return Config{}, fmt.Errorf("WVC_ARGON2_CONCURRENCY must be between 1 and 32")
	}
	if cfg.JobRetention < 0 || cfg.AuditRetention < 0 {
		return Config{}, fmt.Errorf("retention durations cannot be negative")
	}
	if cfg.AuditVerifyTimeout < time.Minute || cfg.AuditVerifyTimeout > 24*time.Hour {
		return Config{}, fmt.Errorf("WVC_AUDIT_VERIFY_TIMEOUT must be between 1m and 24h")
	}
	if err := validateConsoleLoopbackAddress(cfg.ConsoleLoopbackAddress); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel != "debug" && cfg.LogLevel != "info" && cfg.LogLevel != "warn" && cfg.LogLevel != "error" {
		return Config{}, fmt.Errorf("WVC_LOG_LEVEL must be debug, info, warn or error")
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return Config{}, fmt.Errorf("WVC_LOG_FORMAT must be text or json")
	}
	if cfg.MetricsToken != "" {
		if len(cfg.MetricsToken) < 32 || len(cfg.MetricsToken) > 512 || strings.IndexFunc(cfg.MetricsToken, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII
		}) >= 0 {
			return Config{}, fmt.Errorf("WVC_METRICS_TOKEN must be 32-512 printable bytes when set")
		}
	}
	return cfg, nil
}

func validateListenAddress(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("WVC_ADDR must be a host:port listen address: %w", err)
	}
	if strings.ContainsAny(host, " \t\r\n/\\") {
		return fmt.Errorf("WVC_ADDR contains an invalid host")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("WVC_ADDR port must be between 1 and 65535")
	}
	return nil
}

func validateConsoleLoopbackAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > 253 || strings.ContainsAny(value, " \t\r\n/\\@?#[]") {
		return fmt.Errorf("WVC_CONSOLE_LOOPBACK_ADDRESS must be a hostname or IP address without a port")
	}
	if net.ParseIP(value) != nil {
		return nil
	}
	value = strings.TrimSuffix(value, ".")
	if value == "" || strings.HasPrefix(value, "-") {
		return fmt.Errorf("WVC_CONSOLE_LOOPBACK_ADDRESS is invalid")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("WVC_CONSOLE_LOOPBACK_ADDRESS is invalid")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return fmt.Errorf("WVC_CONSOLE_LOOPBACK_ADDRESS is invalid")
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func csvEnv(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func boolEnv(key string, fallback bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return b, nil
}

func intEnv(key string, fallback int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return d, nil
}
