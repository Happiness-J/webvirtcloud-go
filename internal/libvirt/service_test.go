package libvirt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

func TestParseTableDomains(t *testing.T) {
	output := ` Id   Name         State
------------------------------
 7    web-prod     running
 -    db-test      shut off
`
	rows := ParseTable(output)
	if len(rows) != 2 || rows[0][1] != "web-prod" || rows[1][1] != "db-test" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestParseKeyValue(t *testing.T) {
	got := ParseKeyValue("Id:             7\nState:          running\nCPU(s):         4\n")
	if got["Id"] != "7" || got["State"] != "running" || got["CPU(s)"] != "4" {
		t.Fatalf("unexpected map: %#v", got)
	}
}

func TestParseTablePreservesSpacesInsideColumns(t *testing.T) {
	output := ` Name                 Path
--------------------------------------------------------------
 disk image.img       /var/lib/libvirt/images/my  disk image.img
`
	rows := ParseTable(output)
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if rows[0][0] != "disk image.img" || rows[0][1] != "/var/lib/libvirt/images/my  disk image.img" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}

func TestParseTablePreservesSnapshotColumns(t *testing.T) {
	output := ` Name                 Creation Time               State
-----------------------------------------------------------------
 before upgrade       2026-10-31 12:34:56 +0800   shut off
 disk  checkpoint     2026-11-01 01:02:03 +0800   running
`
	rows := ParseTable(output)
	if len(rows) != 2 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if rows[0][0] != "before upgrade" || rows[0][1] != "2026-10-31 12:34:56 +0800" || rows[0][2] != "shut off" {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1][0] != "disk  checkpoint" || rows[1][1] != "2026-11-01 01:02:03 +0800" || rows[1][2] != "running" {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
}

func TestParseBlockDevices(t *testing.T) {
	output := ` Type   Device   Target   Source
------------------------------------------------------------
 file   disk     vda      /var/lib/libvirt/images/vm.qcow2
 file   cdrom    sda      -
`
	devices := ParseBlockDevices(output)
	if len(devices) != 1 {
		t.Fatalf("devices = %#v", devices)
	}
	if devices[0].Type != "file" || devices[0].Device != "disk" ||
		devices[0].Target != "vda" || devices[0].Source != "/var/lib/libvirt/images/vm.qcow2" {
		t.Fatalf("unexpected device: %#v", devices[0])
	}
}

func TestParseDomainStats(t *testing.T) {
	output := `Domain: 'web-prod'
  state.state=1
  state.reason=1
  cpu.time=180000000000
  balloon.current=2097152
  balloon.maximum=4194304
  vcpu.current=4
  vcpu.maximum=8
  block.count=2
  block.0.capacity=21474836480
  block.0.rd.bytes=1048576
  block.0.wr.bytes=2097152
  block.1.rd.bytes=1024
  block.1.wr.bytes=2048

Domain: 'db-staging'
  state.state=5
  state.reason=1
  balloon.maximum=4194304
  vcpu.maximum=4
`
	stats := ParseDomainStats(output)
	if len(stats) != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if stats[0].Name != "web-prod" || stats[0].State != "running" ||
		stats[0].BalloonCurrent != 2097152 || stats[0].BalloonMaximum != 4194304 ||
		stats[0].CPUTimeNanos != 180000000000 || stats[0].BlockCapacity != 21474836480 ||
		stats[0].BlockReadBytes != 1049600 || stats[0].BlockWriteBytes != 2099200 {
		t.Fatalf("unexpected running stats: %#v", stats[0])
	}
	if stats[1].Name != "db-staging" || stats[1].State != "shut off" {
		t.Fatalf("unexpected stopped stats: %#v", stats[1])
	}
}

func TestParseKiBValue(t *testing.T) {
	if got := parseKiBValue("4194304 KiB"); got != 4194304 {
		t.Fatalf("parseKiBValue() = %d", got)
	}
	for _, value := range []string{"", "unknown", "4194304 MiB"} {
		if got := parseKiBValue(value); got != 0 {
			t.Errorf("parseKiBValue(%q) = %d, want 0", value, got)
		}
	}
}

func TestDomainFromInfoPopulatesResourceFields(t *testing.T) {
	domain := domainFromInfo("web-01", map[string]string{
		"Id":          "7",
		"State":       "running",
		"Autostart":   "enable",
		"CPU(s)":      "4",
		"Max memory":  "8388608 KiB",
		"Used memory": "2097152 KiB",
	})
	if domain.Name != "web-01" || domain.ID != "7" || domain.State != "running" || domain.Autostart != "enable" {
		t.Fatalf("unexpected identity fields: %#v", domain)
	}
	if domain.VCPUs != 4 || domain.MaxMemoryKiB != 8388608 || domain.UsedMemoryKiB != 2097152 {
		t.Fatalf("unexpected resource fields: %#v", domain)
	}
}

func TestValidateName(t *testing.T) {
	for _, name := range []string{"--connect", ".", "..", " vm", "vm/one", "vm:1"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) unexpectedly succeeded", name)
		}
	}
	for _, name := range []string{"vm", "vm with space", "default", ".hidden"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
}

func TestForEachIndexBoundsWorkers(t *testing.T) {
	var active, peak, seen atomic.Int32
	forEachIndex(context.Background(), 50, 4, func(int) {
		current := active.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
		seen.Add(1)
	})
	if got := peak.Load(); got > 4 {
		t.Fatalf("peak concurrency = %d, want <= 4", got)
	}
	if got := seen.Load(); got != 50 {
		t.Fatalf("processed = %d, want 50", got)
	}
}

func TestDetailWarningsReportPartialFailures(t *testing.T) {
	warnings := detailWarnings(errors.New("block"), nil, errors.New("xml"), errors.New("snapshots"), errors.New("backups"), nil, errors.New("memory"), nil, errors.New("guest"), errors.New("power"), errors.New("address"))
	if len(warnings) != 8 {
		t.Fatalf("warnings = %#v, want 8", warnings)
	}
	for _, fragment := range []string{"块设备信息", "XML", "快照", "备份", "guest 内存统计", "guest agent", "电源请求", "客户机 IP"} {
		found := false
		for _, warning := range warnings {
			if strings.Contains(warning, fragment) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("warnings do not mention %s: %#v", fragment, warnings)
		}
	}
	if warnings := detailWarnings(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}

func TestHostToolRequestRunsRemoteVirshThroughSSH(t *testing.T) {
	cipher, err := secret.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	host := store.Host{ID: "host-1", Name: "remote-kvm", Enabled: true}
	host.URICiphertext, err = cipher.SealFor("host:"+host.ID, "qemu+ssh://root@kvm.example:2222/system?keyfile=%2Frun%2Fid_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	request, err := hostToolRequest(cipher, host, "virsh", []string{"list", "--all", "--name"}, true, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if request.SSH == nil || request.SSH.Host != "kvm.example" || request.SSH.Port != 2222 {
		t.Fatalf("request did not use the remote SSH target: %+v", request)
	}
	if request.ConnectURI != "qemu:///system" {
		t.Fatalf("remote virsh connection URI = %q, want qemu:///system", request.ConnectURI)
	}

	dir := t.TempDir()
	capture := filepath.Join(dir, "argv")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + capture + "\"\nprintf 'remote-virsh-ok\\n'\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := command.NewRunner(config.Config{
		SSHBin: fakeSSH, RemotePATH: "/usr/bin:/bin", MaxOutputBytes: 4096,
		MaxArgs: 128, MaxStdinBytes: 4096, CommandTimeout: 5 * time.Second, MaxTimeout: time.Minute,
	})
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "remote-virsh-ok") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{"root@kvm.example", "--connect", "qemu:///system", "virsh", "list"} {
		if !strings.Contains(args, want) {
			t.Errorf("SSH argv missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "qemu+ssh://") {
		t.Fatalf("remote virsh nested qemu+ssh URI:\n%s", args)
	}
}

func TestHostToolRequestRejectsDisabledHost(t *testing.T) {
	cipher, err := secret.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostToolRequest(cipher, store.Host{ID: "host-1", Name: "disabled", Enabled: false}, "virsh", []string{"list"}, true, time.Second); err == nil {
		t.Fatal("disabled host was accepted")
	}
}

func TestHostToolRequestRoutesMultipleRemoteHostsIndependently(t *testing.T) {
	cipher, err := secret.New([]byte(strings.Repeat("m", 32)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "destinations")
	fakeSSH := filepath.Join(dir, "ssh")
	script := `#!/bin/sh
prev=""
dest=""
for arg in "$@"; do
  if [ "$prev" = "--" ]; then dest="$arg"; break; fi
  prev="$arg"
done
printf '%s\n' "$dest" >> "` + capture + `"
case "$dest" in
  root@kvm-a) printf 'host-a\n' ;;
  root@kvm-b) printf 'host-b\n' ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := command.NewRunner(config.Config{
		SSHBin: fakeSSH, RemotePATH: "/usr/bin:/bin", MaxOutputBytes: 4096,
		MaxArgs: 128, MaxStdinBytes: 4096, CommandTimeout: 5 * time.Second, MaxTimeout: time.Minute,
	})
	hosts := []store.Host{
		{ID: "host-a", Name: "kvm-a", Enabled: true},
		{ID: "host-b", Name: "kvm-b", Enabled: true},
	}
	for i, uri := range []string{"qemu+ssh://root@kvm-a/system", "qemu+ssh://root@kvm-b/system"} {
		hosts[i].URICiphertext, err = cipher.SealFor("host:"+hosts[i].ID, uri)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []string{"host-a\n", "host-b\n"} {
		request, err := hostToolRequest(cipher, hosts[i], "virsh", []string{"list", "--all", "--name"}, true, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.Run(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if result.Stdout != want {
			t.Fatalf("host %s output = %q, want %q", hosts[i].Name, result.Stdout, want)
		}
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(raw)); strings.Join(got, ",") != "root@kvm-a,root@kvm-b" {
		t.Fatalf("SSH destinations = %v, want each host routed independently", got)
	}
}

func TestParsePowerRequest(t *testing.T) {
	if got := parsePowerRequest("running (shutdown)"); !strings.Contains(got, "ACPI") {
		t.Fatalf("shutdown request = %q", got)
	}
	if got := parsePowerRequest("running\n"); got != "" {
		t.Fatalf("normal state = %q", got)
	}
}

func TestPNGSignature(t *testing.T) {
	valid := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'x'}
	if !pngSignature(valid) {
		t.Fatal("valid PNG signature rejected")
	}
	if pngSignature([]byte("GIF89a")) {
		t.Fatal("non-PNG accepted")
	}
}

func TestParseGuestAddresses(t *testing.T) {
	output := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 eth0       52:54:00:11:22:33    ipv4         192.0.2.20/24
 lo         00:00:00:00:00:00    ipv6         ::1/128
`
	got := ParseGuestAddresses(output, "guest agent")
	if len(got) != 2 || got[0].Address != "192.0.2.20" || got[0].Prefix != "24" || got[0].Source != "guest agent" {
		t.Fatalf("addresses = %#v", got)
	}
	if got := ParseGuestAddresses("Name  MAC  Protocol  Address\neth0  aa  ipv4  not-an-ip", "lease"); len(got) != 0 {
		t.Fatalf("invalid address accepted: %#v", got)
	}
}
