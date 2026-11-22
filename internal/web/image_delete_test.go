package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteLibraryImageRemovesOnlyNamedFile(t *testing.T) {
	dir := t.TempDir()
	name := "nightly-test.iso"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deleteLibraryImage(dir, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatal("image still exists")
	}
	if err := deleteLibraryImage(dir, "../secret.iso"); err == nil {
		t.Fatal("path escape accepted")
	}
}
