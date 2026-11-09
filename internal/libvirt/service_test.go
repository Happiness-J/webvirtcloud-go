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

func TestValidateName(t *testing.T) {
	for _, name := range []string{"--connect", ".", "..", " vm", "vm/one"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) unexpectedly succeeded", name)
		}
	}
	for _, name := range []string{"vm", "vm with space", "vm:1", ".hidden"} {
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
	warnings := detailWarnings(errors.New("block"), nil, errors.New("xml"), errors.New("snapshots"))
	if len(warnings) != 3 {
		t.Fatalf("warnings = %#v, want 3", warnings)
	}
	for _, fragment := range []string{"块设备", "XML", "快照"} {
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
	if warnings := detailWarnings(nil, nil, nil, nil); len(warnings) != 0 {
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
