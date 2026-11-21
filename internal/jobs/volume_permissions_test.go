package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"webvirtcloud-go/internal/command"
)

func TestCreatedVolumeArgs(t *testing.T) {
	pool, name, ok := createdVolumeArgs([]string{"vol-create-as", "images", "nightly1122perm", "1M"})
	if !ok || pool != "images" || name != "nightly1122perm" {
		t.Fatalf("pool = %q, name = %q, ok = %v", pool, name, ok)
	}
	if _, _, ok = createdVolumeArgs([]string{"vol-create-as", "images", "../disk"}); ok {
		t.Fatal("path traversal accepted")
	}
	if _, _, ok = createdVolumeArgs([]string{"vol-clone", "images", "disk"}); ok {
		t.Fatal("non-create command accepted")
	}
}

func TestVolumePathFromOutput(t *testing.T) {
	path, ok := volumePathFromOutput("\n/var/lib/libvirt/images/nightly1122perm\n")
	if !ok || path != "/var/lib/libvirt/images/nightly1122perm" {
		t.Fatalf("path = %q, ok = %v", path, ok)
	}
	if _, ok = volumePathFromOutput("/tmp/../disk"); ok {
		t.Fatal("path traversal accepted")
	}
}

func TestShareCreatedVolume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disk")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := shareCreatedVolume(path); err == nil {
		t.Fatal("path outside the image directory was accepted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want unchanged 600", info.Mode().Perm())
	}
}

func TestShareLocalCreatedVolumeSkipsRemote(t *testing.T) {
	w := &Worker{}
	w.shareLocalCreatedVolume(command.Target{Kind: command.TargetSSH}, []string{"vol-create-as", "images", "disk"})
}
