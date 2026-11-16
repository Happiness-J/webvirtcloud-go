package web

import "testing"

func TestVideoArg(t *testing.T) {
	got, err := videoArg("qxl", 64, "vnc")
	if err != nil || got != "model=qxl,vram=65536" {
		t.Fatalf("video=%q err=%v", got, err)
	}
	if _, err = videoArg("none", 64, "spice"); err == nil {
		t.Fatal("headless display accepted with SPICE")
	}
	gotDevice := parseVideo(`<domain><devices><video><model type="virtio" vram="65536"/></video></devices></domain>`)
	if gotDevice.Model != "virtio" || gotDevice.VRAM != 64 {
		t.Fatalf("device=%#v", gotDevice)
	}
}
