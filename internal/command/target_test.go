package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/remote"
)

func TestResolveTargetExecutionModes(t *testing.T) {
	sshTarget, err := ResolveTarget("qemu+ssh://root@kvm.example:2222/system?keyfile=%2Frun%2Fid")
	if err != nil {
		t.Fatal(err)
	}
	if sshTarget.Kind != TargetSSH || sshTarget.SSH == nil || sshTarget.SSH.Host != "kvm.example" ||
		sshTarget.SSH.Port != 2222 || sshTarget.ConnectURI != "qemu:///system" {
		t.Fatalf("SSH target = %+v", sshTarget)
	}
	if err := sshTarget.Supports("qemu-img"); err != nil {
		t.Fatalf("SSH target rejected independent QEMU tool: %v", err)
	}
	if request := sshTarget.Request("virsh", []string{"list"}, "", true, time.Second); request.ConnectURI != "qemu:///system" || request.SSH == nil {
		t.Fatalf("virsh request = %+v", request)
	}
	if request := sshTarget.Request("qemu-img", []string{"info", "/x"}, "", true, time.Second); request.ConnectURI != "" || request.SSH == nil {
		t.Fatalf("qemu-img request = %+v", request)
	}

	localTarget, err := ResolveTarget("qemu:///system")
	if err != nil || localTarget.Kind != TargetLocal || localTarget.SSH != nil || localTarget.ConnectURI != "qemu:///system" {
		t.Fatalf("local target = %+v, %v", localTarget, err)
	}
	tcpTarget, err := ResolveTarget("qemu+tcp://kvm.example/system")
	if err != nil || tcpTarget.Kind != TargetLibvirt || tcpTarget.SSH != nil {
		t.Fatalf("TCP target = %+v, %v", tcpTarget, err)
	}
	if err := tcpTarget.Supports("virsh"); err != nil {
		t.Fatalf("libvirt TCP target rejected virsh: %v", err)
	}
	if err := tcpTarget.Supports("qemu-img"); err == nil {
		t.Fatal("libvirt TCP target accepted an independent QEMU tool")
	}
}

func TestRunnerExecutesHostToolsThroughSSH(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "ssh-args")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + capture + "\"\nprintf 'remote-ok\\n'\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := testRunner()
	runner.sshPath = fakeSSH
	runner.remotePATH = "/usr/bin:/bin"
	target := remote.Target{Host: "kvm.example", User: "root"}
	request := targetRequest("virt-builder", []string{"--list"}, target)

	// virt-builder is not installed on the panel in this test. A remote request
	// must still validate and execute instead of checking local paths.
	runner.paths = map[string]string{"virsh": "/bin/true"}
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "remote-ok") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{"BatchMode=yes", "StrictHostKeyChecking=yes", "root@kvm.example", "/bin/sh", "virt-builder"} {
		if !strings.Contains(args, want) {
			t.Errorf("SSH argv missing %q:\n%s", want, args)
		}
	}
}

func TestRunnerRemoteVirshUsesTargetLocalURI(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "ssh-args")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + capture + "\"\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := testRunner()
	runner.sshPath = fakeSSH
	runner.remotePATH = "/usr/bin:/bin"
	target := remote.Target{Host: "kvm.example", User: "root"}
	request := targetRequest("virsh", []string{"list", "--all"}, target)
	request.ConnectURI = "qemu:///system"
	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	if !strings.Contains(args, "--connect") || !strings.Contains(args, "qemu:///system") {
		t.Fatalf("remote virsh did not receive the target-local URI:\n%s", args)
	}
	if strings.Contains(args, "qemu+ssh") {
		t.Fatalf("remote virsh nested qemu+ssh:\n%s", args)
	}
}

func TestRunnerRemoteQemuImgUsesSSH(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "ssh-args")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + capture + "\"\nprintf 'remote-qemu-img-ok\\n'\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := testRunner()
	runner.paths = map[string]string{"qemu-img": "/does/not/exist"}
	runner.sshPath = fakeSSH
	runner.remotePATH = "/usr/bin:/bin"
	target := remote.Target{Host: "kvm.example", User: "root"}
	request := targetRequest("qemu-img", []string{"info", "/var/lib/libvirt/images/vm.qcow2"}, target)
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "remote-qemu-img-ok") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{"root@kvm.example", "qemu-img", "info", "/var/lib/libvirt/images/vm.qcow2"} {
		if !strings.Contains(args, want) {
			t.Errorf("SSH argv missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--connect") {
		t.Fatalf("remote qemu-img received a libvirt connection option:\n%s", args)
	}
}

func TestDiscoverRemoteUsesSinglePresenceProbe(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "calls")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\necho call >> \"" + capture + "\"\nprintf 'virsh\\nqemu-img\\n'\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := testRunner()
	runner.sshPath = fakeSSH
	runner.tools = []Tool{{Name: "virsh", Available: true}, {Name: "qemu-img", Available: true}, {Name: "qemu-io", Available: true}}
	discovery, err := DiscoverRemote(context.Background(), runner, remote.Target{Host: "kvm.example"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Tools["virsh"].Available != true || discovery.Tools["qemu-img"].Available != true {
		t.Fatalf("presence results = %+v", discovery.Tools)
	}
	if discovery.Tools["qemu-io"].Available {
		t.Fatal("qemu-io unexpectedly reported as installed")
	}
	calls, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(calls)), "call") != 1 {
		t.Fatalf("remote discovery used %d SSH calls, want 1", strings.Count(string(calls), "call"))
	}
}

func targetRequest(tool string, args []string, target remote.Target) Request {
	requestTarget := Target{Kind: TargetSSH, SSH: &target, ConnectURI: "qemu:///system", Label: target.Destination()}
	return requestTarget.Request(tool, args, "", true, time.Second)
}
