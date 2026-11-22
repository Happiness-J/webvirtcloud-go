package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectImageFormatUsesQCOWHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disk")
	data := append([]byte("QFI\xfb"), make([]byte, 12)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectImageFormat(path); got != "qcow2" {
		t.Fatalf("format = %q", got)
	}
}
