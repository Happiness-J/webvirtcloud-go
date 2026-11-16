package web

import (
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
