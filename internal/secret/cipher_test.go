package secret

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrCreateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "master.key")
	cipher, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.Seal("qemu+ssh://root@example/system")
	if err != nil {
		t.Fatal(err)
	}
	if sealed == "qemu+ssh://root@example/system" {
		t.Fatal("secret was not encrypted")
	}
	again, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := again.Open(sealed)
	if err != nil || plain != "qemu+ssh://root@example/system" {
		t.Fatalf("open: %q %v", plain, err)
	}
}

func TestCipherPurposeBindingAndLegacyCompatibility(t *testing.T) {
	cipher, err := New(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := cipher.Seal("qemu+ssh://root@example/system")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy, "v1:") {
		t.Fatalf("legacy ciphertext prefix = %q", legacy)
	}
	if _, err := cipher.OpenFor("host:h1", legacy); err == nil {
		t.Fatal("purpose-bound reads accepted unbound legacy ciphertext")
	}
	if got, err := cipher.OpenLegacy(legacy); err != nil || got != "qemu+ssh://root@example/system" {
		t.Fatalf("legacy OpenLegacy = %q, %v", got, err)
	}
	bound, err := cipher.SealFor("host:h1", "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(bound, "v2:") {
		t.Fatalf("bound ciphertext prefix = %q", bound)
	}
	if _, err := cipher.OpenFor("host:h2", bound); err == nil {
		t.Fatal("ciphertext decrypted under the wrong purpose")
	}
	if _, err := cipher.Open(bound); err == nil {
		t.Fatal("v2 ciphertext decrypted without a purpose")
	}
}

func TestLoadOrCreateRejectsUnsafeKeyFiles(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32)) + "\n"
	dir := filepath.Join(t.TempDir(), "keys")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "master.key")
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("world-writable key directory error = %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil || !strings.Contains(err.Error(), "group or others") {
		t.Fatalf("world-readable key file error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.key")
	if err := os.WriteFile(target, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("symlink master key was accepted")
	}
}

func TestConcurrentLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "master.key")
	const workers = 12
	results := make([]*Cipher, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = LoadOrCreate(path)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	sealed, err := results[0].SealFor("host:h1", "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	for i, cipher := range results[1:] {
		if got, err := cipher.OpenFor("host:h1", sealed); err != nil || got != "qemu:///system" {
			t.Fatalf("worker %d did not load the winning key: %q, %v", i+1, got, err)
		}
	}
}
