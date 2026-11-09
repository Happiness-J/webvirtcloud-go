package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/store"
)

func TestRunCLIVersion(t *testing.T) {
	oldVersion, oldCommit, oldBuildDate := version, commit, buildDate
	t.Cleanup(func() {
		version, commit, buildDate = oldVersion, oldCommit, oldBuildDate
	})
	version = "v1.2.3"
	commit = "0123456789abcdef"
	buildDate = "2026-01-02T03:04:05Z"

	for _, arg := range []string{"version", "--version", "-version"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			if err := runCLI([]string{arg}, &out); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"wvc v1.2.3", "commit: 0123456789abcdef", "built: 2026-01-02T03:04:05Z", "platform:"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("version output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestRunCLIHelpAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := runCLI([]string{"help"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wvc doctor") {
		t.Fatalf("help output = %q", out.String())
	}

	out.Reset()
	if err := runCLI([]string{"unknown"}, &out); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown command error = %v", err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("usage was not printed: %q", out.String())
	}

	out.Reset()
	if err := runCLI([]string{"version", "extra"}, &out); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("unexpected argument error = %v", err)
	}
}

func TestRunDoctorRequiresConfiguration(t *testing.T) {
	for _, key := range []string{"WVC_DATABASE_URL", "WVC_ADDR", "WVC_MASTER_KEY_FILE"} {
		t.Setenv(key, "")
	}
	var out bytes.Buffer
	err := runCLI([]string{"doctor"}, &out)
	if err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("doctor error = %v", err)
	}
	if !strings.Contains(out.String(), "WebVirtCloud Go doctor") {
		t.Fatalf("doctor heading missing: %q", out.String())
	}
}

type passwordResetFake struct {
	user       store.User
	newHash    string
	setErr     error
	auditError error
}

func (f *passwordResetFake) GetUserByUsername(context.Context, string) (store.User, error) {
	if f.user.ID == "" {
		return store.User{}, errors.New("not found")
	}
	return f.user, nil
}

func (f *passwordResetFake) SetPassword(_ context.Context, _ string, hash string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.newHash = hash
	return nil
}

func (f *passwordResetFake) AddAuditReliable(context.Context, store.Audit) error {
	return f.auditError
}

func TestResetAdminPassword(t *testing.T) {
	keyDir := t.TempDir()
	fake := &passwordResetFake{user: store.User{ID: "user_test", Username: "admin"}}
	var out bytes.Buffer
	if err := resetAdminPassword(context.Background(), fake, filepath.Join(keyDir, "master.key"), "admin", &out); err != nil {
		t.Fatal(err)
	}
	if fake.newHash == "" || !strings.Contains(out.String(), "password reset for admin") {
		t.Fatalf("reset result: hash=%q output=%q", fake.newHash, out.String())
	}

	matches, err := filepath.Glob(filepath.Join(keyDir, "reset-password-user_test-*.txt"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("password files = %v, err = %v", matches, err)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("password file mode = %o, want 600", info.Mode().Perm())
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(string(content), "\n")
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "password: ") {
		t.Fatalf("unexpected password file: %q", content)
	}
	password := strings.TrimPrefix(fields[1], "password: ")
	if !auth.VerifyPassword(password, fake.newHash) {
		t.Fatal("generated password does not match stored hash")
	}
}

func TestResetAdminPasswordCleansFileOnStoreFailure(t *testing.T) {
	keyDir := t.TempDir()
	fake := &passwordResetFake{
		user:   store.User{ID: "user_test", Username: "admin"},
		setErr: errors.New("database unavailable"),
	}
	err := resetAdminPassword(context.Background(), fake, filepath.Join(keyDir, "master.key"), "admin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "update password") {
		t.Fatalf("reset error = %v", err)
	}
	matches, globErr := filepath.Glob(filepath.Join(keyDir, "reset-password-*.txt"))
	if globErr != nil || len(matches) != 0 {
		t.Fatalf("password file was not cleaned up: %v, %v", matches, globErr)
	}
}
