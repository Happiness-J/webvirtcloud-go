package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListImportableDisks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "disk.qcow2"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listImportableDisks(dir)
	if err != nil || len(got) != 1 || got[0] != "disk.qcow2" {
		t.Fatalf("%v %v", got, err)
	}
}
