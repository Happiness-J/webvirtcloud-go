package web

import (
	"os"
	"strings"
	"testing"
)

func TestLibraryNames(t *testing.T) {
	if !validLibraryName("virtio-win.iso") || libraryCategory("virtio-win.iso") != "virtio" {
		t.Fatal("virtio image rejected")
	}
	if validLibraryName("../disk.qcow2") || validLibraryName("disk.img") {
		t.Fatal("unsafe image name accepted")
	}
	if !strings.Contains(libraryCategory("guest-agent.iso"), "guest-agent") {
		t.Fatal("guest agent category missing")
	}
}

func TestSaveLibraryUploadRemovesPartialFile(t *testing.T) {
	dir := t.TempDir()
	err := saveLibraryUpload(dir, "disk.qcow2", strings.NewReader("abcdef"), 3)
	if err == nil || !strings.Contains(err.Error(), "超过大小限制") {
		t.Fatalf("save error = %v", err)
	}
	if _, statErr := os.Stat(dir + "/disk.qcow2"); !os.IsNotExist(statErr) {
		t.Fatalf("partial upload remains: %v", statErr)
	}
}
