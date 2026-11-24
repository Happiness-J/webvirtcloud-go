package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestVMManifestHidesSecrets(t *testing.T) {
	manifest := vmManifest(libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "running", VCPUs: 2, MaxMemoryKiB: 2048}, BlockList: []libvirt.BlockDevice{{Target: "vda", Source: "qemu+ssh://user:secret@host/system"}}})
	if strings.Contains(manifest, "secret") || strings.Contains(manifest, "qemu+ssh") {
		t.Fatalf("manifest leaked secret: %s", manifest)
	}
	if !strings.Contains(manifest, "name: web") || !strings.Contains(manifest, "[已隐藏]") {
		t.Fatalf("manifest = %s", manifest)
	}
}
