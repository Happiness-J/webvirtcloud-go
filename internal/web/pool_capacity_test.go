package web

import (
	"bytes"
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"

	"webvirtcloud-go/internal/libvirt"
)

func TestPoolCapacityHintFlagsOvercommit(t *testing.T) {
	pools := []libvirt.Pool{{Name: "images", Capacity: "10.0 GB", Available: "8.0 GB"}}
	volumes := []libvirt.Volume{
		{Pool: "images", Name: "a", Capacity: "8.0 GB"},
		{Pool: "images", Name: "b", Capacity: "4.0 GB"},
	}
	got := poolCapacityHints(pools, volumes)["images"]
	if !got.Known || !got.Over || got.Label != "已超配" || got.UsedPercent != 100 {
		t.Fatalf("hint = %#v", got)
	}
	if got.Committed != "12.0 GB" || got.Remaining != "已超出 2.0 GB" {
		t.Fatalf("sizes = %#v", got)
	}
}

func TestPoolCapacityHintShowsRemainder(t *testing.T) {
	pools := []libvirt.Pool{{Name: "images", Capacity: "20.0 GB", Available: "19.0 GB"}}
	volumes := []libvirt.Volume{{Pool: "images", Name: "a", Capacity: "5.0 GB"}}
	got := poolCapacityHints(pools, volumes)["images"]
	if got.Over || got.Label != "未超配" || got.UsedPercent != 25 || got.Remaining != "还剩 15.0 GB" {
		t.Fatalf("hint = %#v", got)
	}
}

func TestPoolCapacityHintWithoutVolumes(t *testing.T) {
	got := poolCapacityHints([]libvirt.Pool{{Name: "images", Capacity: "20.0 GB", Available: "19.0 GB"}}, nil)["images"]
	if got.Known || got.Over || got.Committed != "未统计" {
		t.Fatalf("hint = %#v", got)
	}
}

func TestParseReadableBytesRejectsUnknownUnit(t *testing.T) {
	if _, ok := parseReadableBytes("12 apples"); ok {
		t.Fatal("unknown unit accepted")
	}
}

func TestStoragePageRendersOvercommitHint(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	pools := []libvirt.Pool{{Name: "images", State: "running", Capacity: "10.0 GB", Allocation: "2.0 GB", Available: "8.0 GB", Autostart: "yes", Persistent: "yes"}}
	volumes := []libvirt.Volume{{Pool: "images", Name: "disk", Capacity: "12.0 GB"}}
	page := pageData{Title: "storage", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "Pools": pools, "SelectedPool": "images", "PoolInfo": []kvRow{},
		"Volumes": volumes, "VolumeUsage": map[string][]volumeUsage{}, "CapacityHints": poolCapacityHints(pools, volumes),
	}}
	var rendered bytes.Buffer
	if err := templates["storage"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"已超配", "承诺 12.0 GB", "已超出 2.0 GB", "不阻止创建卷"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
