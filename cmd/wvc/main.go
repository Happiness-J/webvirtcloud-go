package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"webvirtcloud-go/internal/audit"
	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/jobs"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
	"webvirtcloud-go/internal/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

const (
	startupDatabaseTimeout = 5 * time.Minute
	startupDataTimeout     = 30 * time.Minute
	backgroundStopTimeout  = 20 * time.Second
	defaultHealthcheckURL  = "http://127.0.0.1:8080/readyz"
)

func main() {
	if err := runCLI(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wvc:", err)
		os.Exit(1)
	}
}

// healthcheck is a container health probe rather than a second server mode.
// It deliberately avoids loading application configuration or touching the
// database directly, so it validates exactly what an external probe sees.
func runHealthcheck() error {
	raw := strings.TrimSpace(os.Getenv("WVC_HEALTHCHECK_URL"))
	if raw == "" {
		raw = defaultHealthcheckURL
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return fmt.Errorf("WVC_HEALTHCHECK_URL must be an absolute HTTP(S) URL without credentials or fragments")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("WVC_HEALTHCHECK_URL must use http or https")
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// #nosec G704 -- this is an explicit operator-configured healthcheck target, never browser input.
	resp, err := client.Get(target.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(strings.ToValidUTF8(string(body), "�"))
		if message == "" {
			return fmt.Errorf("readiness probe returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("readiness probe returned HTTP %d: %s", resp.StatusCode, message)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, options)
	} else {
		handler = slog.NewTextHandler(os.Stdout, options)
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	auth.ConfigureArgon2(cfg.Argon2Concurrency)
	if !cfg.SecureCookies {
		log.Warn("WVC_SECURE_COOKIES is disabled; use this only for local development or trusted plain-HTTP networks")
	}
	if cfg.AllowInsecureVNC {
		log.Warn("WVC_ALLOW_INSECURE_VNC is enabled; VNC traffic is not encrypted")
	}
	if cfg.AllowInsecureConsole {
		log.Warn("WVC_ALLOW_INSECURE_CONSOLE is enabled; the panel, session cookies and console traffic are exposed to plain HTTP")
	}
	if status := imageDirStatus(cfg.ImageDir); strings.HasPrefix(status, "not writable") {
		log.Warn("local image directory is not writable; offline qemu-img and libguestfs operations will fail",
			"path", cfg.ImageDir, "status", status)
	}
	if cfg.SecureCookies && !cfg.TrustProxy {
		log.Warn("WVC_SECURE_COOKIES is enabled without WVC_TRUST_PROXY; HTTPS reverse-proxy WebSocket consoles will reject same-origin upgrades unless the proxy is trusted")
	}
	if cfg.TrustProxy {
		log.Info("trusted reverse-proxy headers enabled", "cidrs", strings.Join(cfg.TrustedProxyCIDRs, ","))
	}
	if cfg.ConsoleLoopbackAddress != "" {
		log.Info("rewriting local loopback VNC endpoints", "address", cfg.ConsoleLoopbackAddress)
	}

	databaseCtx, databaseCancel := context.WithTimeout(context.Background(), startupDatabaseTimeout)
	db, err := openDatabase(databaseCtx, cfg.DatabaseURL, log)
	if err != nil {
		databaseCancel()
		return err
	}
	defer db.Close()
	if err := db.Migrate(databaseCtx); err != nil {
		databaseCancel()
		return err
	}
	databaseCancel()
	db.SetJobLimits(cfg.MaxActiveJobsPerUser, cfg.MaxActiveJobs)
	cipher, err := secret.LoadOrCreate(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	db.SetCipher(cipher)
	dataCtx, dataCancel := context.WithTimeout(context.Background(), startupDataTimeout)
	defer dataCancel()
	if n, err := db.UpgradeLegacyHostURIs(dataCtx); err != nil {
		return fmt.Errorf("upgrade legacy host encryption: %w", err)
	} else if n > 0 {
		log.Info("upgraded host ciphertext", "count", n)
	}
	if n, err := db.UpgradeLegacyJobCiphertexts(dataCtx, 200); err != nil {
		return fmt.Errorf("upgrade legacy job encryption: %w", err)
	} else if n > 0 {
		log.Info("upgraded job ciphertext", "count", n)
	}
	if n, err := db.EncryptLegacyJobs(dataCtx, 200); err != nil {
		return fmt.Errorf("encrypt legacy job data: %w", err)
	} else if n > 0 {
		log.Info("encrypted legacy job records", "count", n)
	}
	dataCancel()
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), cfg.AuditVerifyTimeout)
	defer verifyCancel()
	if err := db.VerifyAuditChain(verifyCtx); err != nil {
		return fmt.Errorf("verify audit hash chain: %w", err)
	}
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), startupDatabaseTimeout)
	defer bootstrapCancel()
	if err := bootstrapAdmin(bootstrapCtx, db, cfg, log); err != nil {
		return err
	}
	bootstrapCancel()
	runner := command.NewRunner(cfg)
	for _, tool := range runner.Tools() {
		if tool.Available {
			log.Info("command tool available", "name", tool.Name, "path", tool.Path)
		} else {
			log.Warn("command tool missing", "name", tool.Name, "configured", tool.Path)
		}
	}

	recorder := audit.New(db, log)
	server, err := web.New(cfg, db, cipher, runner, recorder, log)
	if err != nil {
		return err
	}
	worker := jobs.New(cfg, db, cipher, runner, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var background sync.WaitGroup
	background.Add(3)
	go func() {
		defer background.Done()
		worker.Run(runCtx)
	}()
	go func() {
		defer background.Done()
		recorder.Run(runCtx)
	}()
	go func() {
		defer background.Done()
		runMaintenance(runCtx, db, cfg, log)
	}()

	log.Info("starting WebVirtCloud Go", "version", version, "addr", cfg.Addr, "workers", cfg.Workers)
	serverErr := server.Run(ctx)
	cancelRun()
	backgroundDone := make(chan struct{})
	go func() {
		background.Wait()
		close(backgroundDone)
	}()
	if err := waitForBackground(backgroundDone, backgroundStopTimeout); err != nil {
		return err
	}
	return serverErr
}

func waitForBackground(done <-chan struct{}, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("timed out waiting for background services to stop")
	}
}

func openDatabase(ctx context.Context, databaseURL string, log *slog.Logger) (*store.Store, error) {
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		db, err := store.Open(ctx, databaseURL)
		if err == nil {
			return db, nil
		}
		lastErr = err
		log.Warn("database not ready", "attempt", attempt, "error", redactDatabaseError(err, databaseURL))
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("open database: %w", errors.New(redactDatabaseError(lastErr, databaseURL)))
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("open database: %w", errors.New(redactDatabaseError(lastErr, databaseURL)))
}

func bootstrapAdmin(ctx context.Context, db *store.Store, cfg config.Config, log *slog.Logger) error {
	count, err := db.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		if cfg.AdminPassword != "" {
			log.Warn("WVC_ADMIN_PASSWORD is still configured after bootstrap; remove it from the service environment and use the password-change or reset-password flow")
		}
		return nil
	}
	password := cfg.AdminPassword
	generated := false
	if password == "" {
		password, err = auth.NewToken()
		if err != nil {
			return err
		}
		generated = true
	}
	hash, err := auth.HashPasswordContext(ctx, password)
	if err != nil {
		return fmt.Errorf("initial admin password: %w", err)
	}
	user := store.User{ID: id.New("user"), Username: cfg.AdminUsername, PasswordHash: hash, Role: "admin"}
	passwordFile := ""
	if generated {
		passwordFile = filepath.Join(filepath.Dir(cfg.MasterKeyFile), "initial-admin-password-"+user.ID+".txt")
		content := fmt.Sprintf("username: %s\npassword: %s\n\nChange this password after the first login and delete this file.\n", cfg.AdminUsername, password)
		if err := writePrivateFile(passwordFile, []byte(content)); err != nil {
			return fmt.Errorf("write initial admin password file: %w", err)
		}
	}
	created, err := db.EnsureInitialAdmin(ctx, user)
	if err != nil {
		if passwordFile != "" {
			_ = os.Remove(passwordFile)
		}
		return fmt.Errorf("create initial admin: %w", err)
	}
	if !created {
		if passwordFile != "" {
			_ = os.Remove(passwordFile)
		}
		return nil
	}
	if err := db.AddAuditReliable(ctx, store.Audit{
		ActorName: "bootstrap", Action: "bootstrap.admin_created",
		Detail: map[string]any{"username": user.Username},
	}); err != nil {
		log.Warn("initial admin created but bootstrap audit write failed", "username", user.Username, "error", err)
	}
	if generated {
		log.Warn("initial admin created; read the one-time password from a mode-0600 file", "username", cfg.AdminUsername, "password_file", passwordFile)
	} else {
		log.Info("initial admin created from WVC_ADMIN_PASSWORD", "username", cfg.AdminUsername)
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	// #nosec G304 -- path is constructed beside the trusted, operator-configured master-key path.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func redactDatabaseError(err error, databaseURL string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if parsed, parseErr := url.Parse(databaseURL); parseErr == nil && parsed.User != nil {
		if password, set := parsed.User.Password(); set && password != "" {
			message = strings.ReplaceAll(message, password, "***")
		}
		parsed.User = url.User(parsed.User.Username())
		message = strings.ReplaceAll(message, databaseURL, parsed.String())
	}
	return message
}

func runMaintenance(ctx context.Context, db *store.Store, cfg config.Config, log *slog.Logger) {
	cleanup := func() {
		workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if cfg.JobRetention > 0 {
			if n, err := db.PurgeOldJobs(workCtx, time.Now().Add(-cfg.JobRetention)); err != nil {
				log.Error("purge old jobs", "error", err)
			} else if n > 0 {
				log.Info("purged old jobs", "count", n)
			}
		}
		if cfg.AuditRetention > 0 {
			if n, err := db.PurgeOldAudit(workCtx, time.Now().Add(-cfg.AuditRetention)); err != nil {
				log.Error("purge old audit entries", "error", err)
			} else if n > 0 {
				log.Info("purged old audit entries", "count", n)
			}
		}
	}
	cleanup()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
