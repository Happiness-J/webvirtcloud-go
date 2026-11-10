package web

import (
	"reflect"
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestNormalizeBatchVMNames(t *testing.T) {
	got, err := normalizeBatchVMNames([]string{" web-prod ", "", "db", "web-prod"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"web-prod", "db"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeBatchVMNames() = %#v, want %#v", got, want)
	}
	if _, err := normalizeBatchVMNames([]string{"--connect"}); err == nil {
		t.Fatal("normalizeBatchVMNames accepted an option-like name")
	}
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "vm-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := normalizeBatchVMNames(tooMany); err == nil {
		t.Fatal("normalizeBatchVMNames accepted more than 50 VMs")
	}
}

func TestBatchActionCompatibility(t *testing.T) {
	tests := []struct {
		action, state string
		want          bool
	}{
		{"start", "shut off", true},
		{"start", "running", false},
		{"shutdown", "running", true},
		{"shutdown", "paused", true},
		{"shutdown", "shut off", false},
		{"reboot", "running", true},
		{"reboot", "paused", false},
		{"resume", "paused", true},
		{"resume", "running", false},
		{"autostart-on", "shut off", true},
		{"autostart-off", "running", true},
		{"destroy", "running", false},
	}
	for _, tc := range tests {
		if got := batchActionCompatible(tc.action, tc.state); got != tc.want {
			t.Errorf("batchActionCompatible(%q, %q) = %v, want %v", tc.action, tc.state, got, tc.want)
		}
	}
}

func TestVMResourceArgs(t *testing.T) {
	tests := []struct {
		name, kind, scope, value, state string
		wantArgs                        []string
		wantErr                         bool
	}{
		{name: "cpu config", kind: "cpu", scope: "config", value: "4", state: "shut off", wantArgs: []string{"setvcpus", "vm", "4", "--config"}},
		{name: "cpu live", kind: "cpu", scope: "live", value: "8", state: "running", wantArgs: []string{"setvcpus", "vm", "8", "--live", "--config"}},
		{name: "cpu max", kind: "cpu-max", scope: "live", value: "16", state: "shut off", wantArgs: []string{"setvcpus", "vm", "16", "--maximum", "--config"}},
		{name: "memory", kind: "memory", scope: "live", value: "8G", state: "paused", wantArgs: []string{"setmem", "vm", "8388608", "--live", "--config"}},
		{name: "live stopped", kind: "memory", scope: "live", value: "8G", state: "shut off", wantErr: true},
		{name: "bad cpu", kind: "cpu", scope: "config", value: "0", state: "running", wantErr: true},
		{name: "bad memory", kind: "memory", scope: "config", value: "4X", state: "running", wantErr: true},
		{name: "too small memory", kind: "memory", scope: "config", value: "32M", state: "running", wantErr: true},
		{name: "unknown kind", kind: "disk", scope: "config", value: "4", state: "running", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args, _, err := vmResourceArgs(tc.kind, "vm", tc.scope, tc.value, tc.state)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("vmResourceArgs() = %#v, nil; want error", args)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("vmResourceArgs() = %#v, %v; want %#v", args, err, tc.wantArgs)
			}
		})
	}
}

func TestBuildOpsVMRows(t *testing.T) {
	domains := []libvirt.Domain{
		{Name: "web", ID: "7", State: "running", Autostart: "enable", VCPUs: 4, UsedMemoryKiB: 1 << 20, MaxMemoryKiB: 4 << 20},
		{Name: "db", State: "shut off", Autostart: "disable", VCPUs: 2, UsedMemoryKiB: 512 << 10, MaxMemoryKiB: 1 << 20},
	}
	stats := []libvirt.DomainResourceStats{{Name: "web", State: "running", BalloonCurrent: 2 << 20, BalloonMaximum: 4 << 20, CPUTimeNanos: 180000000000}}
	rows := buildOpsVMRows(domains, stats)
	if len(rows) != 2 || rows[0].Name != "db" || rows[1].Name != "web" {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[1].VCPU != "4" || rows[1].MemoryCurrent != "2.0 GiB" || rows[1].CPUTime != "3m" || rows[1].Autostart != "enable" {
		t.Fatalf("unexpected web row: %#v", rows[1])
	}
}
