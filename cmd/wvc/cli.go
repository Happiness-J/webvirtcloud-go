package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/store"
)

func runCLI(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return run()
	}
	if len(args) != 1 {
		printUsage(stdout)
		return fmt.Errorf("unexpected arguments: %s", strings.Join(args[1:], " "))
	}

	switch args[0] {
	case "healthcheck":
		return runHealthcheck()
	case "doctor":
		return runDoctor(stdout)
	case "reset-password":
		return runPasswordReset(stdout)
	case "version", "--version", "-version":
		printVersion(stdout)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		printUsage(stdout)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  wvc                 Start the WebVirtCloud Go server
  wvc healthcheck     Check the configured readiness endpoint
  wvc doctor          Validate configuration and local dependencies
  wvc reset-password  Reset the configured admin password and create a one-time file
  wvc version         Print version and build information
  wvc help            Print this help text

Configuration is read from WVC_* environment variables.`)
}

type passwordResetStore interface {
	GetUserByUsername(context.Context, string) (store.User, error)
	SetPassword(context.Context, string, string) error
	AddAuditReliable(context.Context, store.Audit) error
}

func runPasswordReset(w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", errorsRedactedDatabase(err, cfg.DatabaseURL))
	}
	defer db.Close()
	return resetAdminPassword(ctx, db, cfg.MasterKeyFile, cfg.AdminUsername, w)
}

func resetAdminPassword(ctx context.Context, db passwordResetStore, masterKeyFile, username string, w io.Writer) error {
	user, err := db.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("find admin user %q: %w", username, err)
	}
	password, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("generate password: %w", err)
	}
	hash, err := auth.HashPasswordContext(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	passwordFile := filepath.Join(
		filepath.Dir(masterKeyFile),
		"reset-password-"+user.ID+"-"+filepath.Base(strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", ""))+".txt",
	)
	content := fmt.Sprintf(
		"username: %s\npassword: %s\n\nThis password reset invalidated all existing sessions.\nChange the password after login and delete this file.\n",
		user.Username, password,
	)
	if err := writePrivateFile(passwordFile, []byte(content)); err != nil {
		return fmt.Errorf("write reset password file: %w", err)
	}
	if err := db.SetPassword(ctx, user.ID, hash); err != nil {
		_ = os.Remove(passwordFile)
		return fmt.Errorf("update password: %w", err)
	}
	if err := db.AddAuditReliable(ctx, store.Audit{
		ActorName: "cli",
		Action:    "bootstrap.password_reset",
		Detail:    map[string]any{"username": user.Username, "sessions_revoked": true},
	}); err != nil {
		fmt.Fprintf(w, "warning: password reset succeeded but audit write failed: %v\n", err)
	}
	fmt.Fprintf(w, "password reset for %s\nread the one-time password from: %s\n", user.Username, passwordFile)
	return nil
}

func printVersion(w io.Writer) {
	revision := commit
	if revision == "" || revision == "unknown" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && setting.Value != "" {
					revision = setting.Value
					break
				}
			}
		}
	}
	fmt.Fprintf(w, "wvc %s\n", version)
	fmt.Fprintf(w, "commit: %s\n", revision)
	fmt.Fprintf(w, "built: %s\n", buildDate)
	fmt.Fprintf(w, "go: %s\n", runtime.Version())
	fmt.Fprintf(w, "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func runDoctor(w io.Writer) error {
	fmt.Fprintln(w, "WebVirtCloud Go doctor")
	printVersion(w)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	fmt.Fprintf(w, "config: ok (listen=%s)\n", cfg.Addr)

	keyStatus := "not created yet"
	if info, statErr := os.Stat(cfg.MasterKeyFile); statErr == nil {
		switch {
		case !info.Mode().IsRegular():
			keyStatus = "invalid: not a regular file"
		case info.Mode().Perm()&0o077 != 0:
			keyStatus = "invalid: group/other permissions are set"
		default:
			keyStatus = "ok"
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("master key: %w", statErr)
	}
	fmt.Fprintf(w, "master-key: %s (%s)\n", keyStatus, filepath.Clean(cfg.MasterKeyFile))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", errorsRedactedDatabase(err, cfg.DatabaseURL))
	}
	db.Close()
	fmt.Fprintln(w, "database: ok")

	runner := command.NewRunner(cfg)
	requiredMissing := make([]string, 0)
	for _, tool := range runner.Tools() {
		status := "ok"
		if !tool.Available {
			status = "missing"
			if tool.Required {
				requiredMissing = append(requiredMissing, tool.Name)
			}
		}
		fmt.Fprintf(w, "tool: %-7s %-28s %s\n", status, tool.Name, tool.Purpose)
	}
	if !runner.SSHAvailable() {
		fmt.Fprintln(w, "tool: missing openssh-client")
	}
	if len(requiredMissing) > 0 {
		fmt.Fprintf(w, "warning: required local tools are missing: %s\n", strings.Join(requiredMissing, ", "))
	}
	fmt.Fprintln(w, "doctor: complete")
	return nil
}

func errorsRedactedDatabase(err error, databaseURL string) error {
	return fmt.Errorf("%s", redactDatabaseError(err, databaseURL))
}
