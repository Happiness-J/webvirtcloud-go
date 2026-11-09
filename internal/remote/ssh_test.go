package remote

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFromURIAndValidation(t *testing.T) {
	target, err := FromURI("qemu+ssh://root@kvm.example:2222/system?keyfile=%2Frun%2Fid&known_hosts=%2Frun%2Fknown")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "kvm.example" || target.User != "root" || target.Port != 2222 {
		t.Fatalf("target = %+v", target)
	}
	if target.KeyFile != "/run/id" || target.KnownHosts != "/run/known" {
		t.Fatalf("SSH materials = %+v", target)
	}
	for _, uri := range []string{
		"qemu+ssh://-oProxyCommand=x/system",
		"qemu+ssh://root@host:0/system",
		"qemu+ssh://root@host/system?keyfile=relative",
		"qemu+tcp://host/system",
	} {
		if _, err := FromURI(uri); err == nil {
			t.Errorf("FromURI(%q) unexpectedly succeeded", uri)
		}
	}
}

func TestQuoteAndExecScriptDoNotInterpretArguments(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "argv")
	tool := filepath.Join(dir, "qemu-img")
	script := "#!/bin/sh\nprintf '%s\\000' \"$@\" > " + Quote(output) + "\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"info", "a b", "x'; touch " + filepath.Join(dir, "pwn") + "; '", "$(touch " + filepath.Join(dir, "pwn2") + ")", "line\nbreak"}
	command := ExecScript(dir, append([]string{"qemu-img"}, args...))
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v: %s", err, out)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if strings.Join(got, "\x00") != strings.Join(args, "\x00") {
		t.Fatalf("argv = %#v, want %#v", got, args)
	}
	for _, name := range []string{"pwn", "pwn2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("argument injected a file named %s", name)
		}
	}
}

func TestCommandAndStreamArgsAreHardened(t *testing.T) {
	target := Target{Host: "kvm.example", User: "root", Port: 2222, KeyFile: "/run/id", KnownHosts: "/run/known"}
	args, err := target.CommandArgs("exec 'qemu-img' 'info' '/a b'")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"BatchMode=yes", "StrictHostKeyChecking=yes", "ConnectTimeout=10",
		"ServerAliveInterval=15", "RequestTTY=no", "ClearAllForwardings=yes",
		"IdentitiesOnly=yes", "UserKnownHostsFile=/run/known", "-p 2222",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("SSH args missing %q: %q", want, args)
		}
	}
	if args[len(args)-5] != "--" || args[len(args)-4] != "root@kvm.example" || args[len(args)-3] != "/bin/sh" || args[len(args)-2] != "-c" || args[len(args)-1] == "" {
		t.Fatalf("SSH command shape = %#v", args[len(args)-5:])
	}
	stream, err := target.StreamArgs("127.0.0.1", "5901")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(stream, "-W", "127.0.0.1:5901") || stream[len(stream)-1] != "root@kvm.example" {
		t.Fatalf("stream args = %#v", stream)
	}
	presence := PresenceScript("/usr/bin:/bin", []string{"virsh", "qemu-img"})
	if !strings.Contains(presence, "'virsh'") || !strings.Contains(presence, "'qemu-img'") {
		t.Fatalf("presence script = %q", presence)
	}
}

func TestExecScriptPreservesStdin(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "stdin")
	tool := filepath.Join(dir, "reader")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n/bin/cat > "+Quote(output)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", ExecScript(dir, []string{"reader"}))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	cmd.Stdin = bytes.NewBufferString("stateful qemu-io input")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v: %s", err, out)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "stateful qemu-io input" {
		t.Fatalf("stdin = %q", raw)
	}
}

func TestExecScriptCancelsRemoteProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not available")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	tool := filepath.Join(dir, "worker")
	script := "#!/bin/sh\nsleep 30 &\necho $! > " + Quote(pidFile) + "\nwait\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	command := ExecScript(dir, []string{"worker"})
	if !strings.Contains(command, "setsid") {
		t.Fatalf("wrapper does not use setsid: %q", command)
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "LC_ALL=C"}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	}()

	childPID := waitForPIDFile(t, pidFile)
	defer func() {
		_ = syscall.Kill(-childPID, syscall.SIGKILL)
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	}()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("remote wrapper did not exit after SIGTERM; output=%s", output.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for processAlive(childPID) {
		if time.Now().After(deadline) {
			t.Fatalf("remote grandchild %d survived wrapper cancellation; output=%s", childPID, output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr != nil || pid <= 1 {
				t.Fatalf("invalid child pid %q: %v", raw, parseErr)
			}
			return pid
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child pid file was not created: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return err != syscall.ESRCH
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(raw))
	return len(fields) < 3 || fields[2] != "Z"
}

func containsPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}
