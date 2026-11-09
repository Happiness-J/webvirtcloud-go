package command

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/config"
)

func testRunner() *Runner {
	return &Runner{
		paths:          map[string]string{"virsh": "/bin/true", "qemu-img": "/bin/true"},
		maxOutputBytes: 4096,
		maxArgs:        128,
		maxStdinBytes:  1024,
		defaultTimeout: time.Second,
		maxTimeout:     time.Minute,
	}
}

func TestRunnerMarksOptionalQemuTools(t *testing.T) {
	virtBins := make(map[string]string, len(config.VirtToolNames))
	for _, name := range config.VirtToolNames {
		virtBins[name] = "missing-" + name
	}
	cfg := config.Config{
		VirshBin: "missing-virsh", QemuImgBin: "missing-qemu-img", QemuIOBin: "missing-qemu-io",
		QemuNbdBin: "missing-qemu-nbd", QemuStorageDaemonBin: "missing-qemu-storage-daemon",
		QemuPRHelperBin: "missing-qemu-pr-helper", QemuTraceStapBin: "missing-qemu-trace-stap",
		QemuVMSRHelperBin: "missing-qemu-vmsr-helper", QemuVNCBin: "missing-qemu-vnc",
		VirtBins: virtBins,
	}
	tools := NewRunner(cfg).Tools()
	wantCount := 9 + len(config.VirtToolNames)
	if len(tools) != wantCount {
		t.Fatalf("tool count = %d, want %d", len(tools), wantCount)
	}
	wantRequired := map[string]bool{
		"virsh": true, "qemu-img": true, "qemu-io": true, "qemu-nbd": true,
		"qemu-storage-daemon": false, "qemu-pr-helper": false, "qemu-trace-stap": false,
		"qemu-vmsr-helper": false, "qemu-vnc": false,
	}
	for _, tool := range tools {
		if tool.Available {
			t.Errorf("%s unexpectedly available", tool.Name)
		}
		want, qemuTool := wantRequired[tool.Name]
		if !qemuTool {
			want = false
		}
		if tool.Required != want {
			t.Errorf("%s Required = %v, want %v", tool.Name, tool.Required, wantRequired[tool.Name])
		}
	}
}

func TestRunnerSSHAvailability(t *testing.T) {
	runner := NewRunner(config.Config{SSHBin: "sh"})
	if !runner.SSHAvailable() {
		t.Fatal("SSHAvailable() = false for an installed OpenSSH-compatible client")
	}
	runner = NewRunner(config.Config{SSHBin: "wvc-missing-openssh-client"})
	if runner.SSHAvailable() {
		t.Fatal("SSHAvailable() = true for a missing OpenSSH client")
	}
}

func TestVirtToolsUseHostConnectionWithoutAllowingOverride(t *testing.T) {
	r := testRunner()
	r.paths["virt-install"] = "/bin/true"
	r.paths["virt-cat"] = "/bin/true"
	if err := r.Validate(Request{Tool: "virt-install", Args: []string{"--name", "vm"}, ConnectURI: "qemu:///system"}); err != nil {
		t.Fatalf("virt-install with host URI: %v", err)
	}
	if err := r.Validate(Request{Tool: "virt-install", Args: []string{"--connect", "qemu+ssh://evil/system", "--name", "vm"}}); err == nil {
		t.Fatal("virt-install accepted --connect override")
	}
	// virt-install uses -c for --cdrom, not --connect.
	if err := r.Validate(Request{Tool: "virt-install", Args: []string{"-c", "/isos/install.iso", "--name", "vm"}, ConnectURI: "qemu:///system"}); err != nil {
		t.Fatalf("virt-install -c cdrom rejected: %v", err)
	}
	if err := r.Validate(Request{Tool: "virt-cat", Args: []string{"-c", "qemu+ssh://evil/system", "file"}}); err == nil {
		t.Fatal("libguestfs accepted -c override")
	}
	r.paths["virt-xml"] = "/bin/true"
	if err := r.Validate(Request{Tool: "virt-xml", Args: []string{"-c", "qemu+ssh://evil/system", "vm", "--print"}}); err == nil {
		t.Fatal("virt-xml accepted -c override")
	}
	result, err := r.Run(context.Background(), Request{Tool: "virt-install", Args: []string{"--name", "vm"}, ConnectURI: "qemu:///system"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"virt-install", "--connect", "qemu:///system", "--name", "vm"}
	if strings.Join(result.Command, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command = %v, want %v", result.Command, want)
	}
}

func TestDiskOnlyVirtToolsRunLocallyWithoutHostURI(t *testing.T) {
	r := testRunner()
	for _, tool := range []string{"virt-format", "virt-resize", "virt-sparsify"} {
		r.paths[tool] = "/bin/true"
		if UsesHostURI(tool) {
			t.Errorf("%s must not be bound to a libvirt URI", tool)
		}
		args := []string{"--help"}
		if err := r.Validate(Request{Tool: tool, Args: args}); err != nil {
			t.Errorf("Validate(%s) without URI = %v", tool, err)
		}
		if err := r.Validate(Request{Tool: tool, Args: args, ConnectURI: "qemu:///system"}); err == nil {
			t.Errorf("%s accepted a libvirt URI even though it only handles local disks", tool)
		}
		result, err := r.Run(context.Background(), Request{Tool: tool, Args: args})
		if err != nil {
			t.Errorf("Run(%s) = %v", tool, err)
			continue
		}
		if strings.Join(result.Command, "\x00") != strings.Join(append([]string{tool}, args...), "\x00") {
			t.Errorf("Run(%s) command = %v, want no injected connection URI", tool, result.Command)
		}
	}
}

func TestValidateRejectsInteractiveCommands(t *testing.T) {
	r := testRunner()
	r.paths["virt-rescue"] = "/bin/true"
	r.paths["virt-login-shell"] = "/bin/true"
	for _, tc := range []struct {
		name string
		req  Request
	}{
		{name: "virsh console", req: Request{Tool: "virsh", Args: []string{"console", "vm"}}},
		{name: "virsh console behind global option", req: Request{Tool: "virsh", Args: []string{"--quiet", "console", "vm"}}},
		{name: "virsh abbreviated console", req: Request{Tool: "virsh", Args: []string{"cons", "vm"}}},
		{name: "virsh abbreviated console behind global option", req: Request{Tool: "virsh", Args: []string{"--quiet", "con", "vm"}}},
		{name: "virsh abbreviated help", req: Request{Tool: "virsh", Args: []string{"hel"}}},
		{name: "virsh abbreviated event", req: Request{Tool: "virsh", Args: []string{"eve"}}},
		{name: "virsh help", req: Request{Tool: "virsh", Args: []string{"help"}}},
		{name: "virt-rescue", req: Request{Tool: "virt-rescue", Args: []string{"-a", "/tmp/disk.img"}, ConnectURI: "qemu:///system"}},
		{name: "virt-login-shell", req: Request{Tool: "virt-login-shell", Args: []string{"--shell"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if UsesHostURI(tc.req.Tool) && tc.req.ConnectURI == "" {
				tc.req.ConnectURI = "qemu:///system"
			}
			err := r.Validate(tc.req)
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("Validate() error = %v, want blocked command", err)
			}
		})
	}
	for _, tc := range []Request{
		{Tool: "virsh", Args: []string{"--help"}, ConnectURI: "qemu:///system"},
		{Tool: "virt-rescue", Args: []string{"--help"}, ConnectURI: "qemu:///system"},
	} {
		if err := r.Validate(tc); err != nil {
			t.Errorf("Validate(%v) rejected informational invocation: %v", tc.Args, err)
		}
	}
}

func TestValidateAllowsOnlyDocumentedZeroArgumentCommands(t *testing.T) {
	r := testRunner()
	r.paths["virt-what"] = "/bin/true"
	r.paths["libguestfs-test-tool"] = "/bin/true"
	if err := r.Validate(Request{Tool: "virt-what"}); err != nil {
		t.Fatalf("virt-what zero-argument invocation rejected: %v", err)
	}
	if err := r.Validate(Request{Tool: "libguestfs-test-tool"}); err != nil {
		t.Fatalf("libguestfs-test-tool zero-argument invocation rejected: %v", err)
	}
	if err := r.Validate(Request{Tool: "virsh"}); err == nil {
		t.Fatal("virsh without a command unexpectedly accepted")
	}
	if readOnly, known := Classify("virt-what", nil); !readOnly || !known {
		t.Fatalf("virt-what zero-argument classification = readOnly=%v known=%v", readOnly, known)
	}
	if readOnly, known := Classify("libguestfs-test-tool", nil); !readOnly || !known {
		t.Fatalf("libguestfs-test-tool zero-argument classification = readOnly=%v known=%v", readOnly, known)
	}
	if sensitive, _ := Sensitive("virt-what", nil); sensitive {
		t.Fatal("virt-what zero-argument invocation must not be sensitive")
	}
}

func TestValidateRejectsConnectOverride(t *testing.T) {
	r := testRunner()
	err := r.Validate(Request{Tool: "virsh", Args: []string{"--connect", "qemu+ssh://evil/system", "list"}, ConnectURI: "qemu:///system"})
	if err == nil || !strings.Contains(err.Error(), "managed by the host profile") {
		t.Fatalf("expected connect override rejection, got %v", err)
	}
}

func TestValidateRejectsShortConnectOverride(t *testing.T) {
	r := testRunner()
	if err := r.Validate(Request{Tool: "virsh", Args: []string{"-cqemu+ssh://evil/system", "list"}, ConnectURI: "qemu:///system"}); err == nil {
		t.Fatal("expected short connect override rejection")
	}
}

func TestValidateRejectsAbbreviatedConnectOverride(t *testing.T) {
	r := testRunner()
	for _, arg := range []string{
		"--c", "--co", "--con", "--conn", "--conne", "--connec", "--connect",
		"--con=qemu+ssh://evil/system", "--connect=qemu+ssh://evil/system",
	} {
		t.Run(arg, func(t *testing.T) {
			err := r.Validate(Request{Tool: "virsh", Args: []string{arg, "list"}, ConnectURI: "qemu:///system"})
			if err == nil || !strings.Contains(err.Error(), "managed by the host profile") {
				t.Fatalf("Validate(%q) = %v, want connect override rejection", arg, err)
			}
		})
	}
}

func TestValidateAllowsUnrelatedVirshLongOptions(t *testing.T) {
	r := testRunner()
	for _, arg := range []string{"--all", "--details", "--domain"} {
		if err := r.Validate(Request{Tool: "virsh", Args: []string{arg, "list"}, ConnectURI: "qemu:///system"}); err != nil {
			t.Errorf("Validate(%q) = %v", arg, err)
		}
	}
}

func TestValidateRejectsConnectSubcommand(t *testing.T) {
	r := testRunner()
	for _, args := range [][]string{
		{"connect", "qemu+ssh://evil/system"},
		{"conn", "qemu+ssh://evil/system"},
		{"connec", "qemu+ssh://evil/system"},
		{"--", "connect", "qemu+ssh://evil/system"},
		{"--quiet", "connect", "qemu+ssh://evil/system"},
		{"--debug", "1", "connect", "qemu+ssh://evil/system"},
		{"--deb", "1", "connect", "qemu+ssh://evil/system"},
		{"-d1", "connect", "qemu+ssh://evil/system"},
		{"-eDEBUG", "connect", "qemu+ssh://evil/system"},
		{"-k5", "connect", "qemu+ssh://evil/system"},
		{"-K1", "connect", "qemu+ssh://evil/system"},
		{"-l/tmp/virsh.log", "connect", "qemu+ssh://evil/system"},
		{"-qc", "qemu+ssh://evil/system", "list"},
		{"-qcqemu+ssh://evil/system", "list"},
		{"-qr", "connect", "qemu+ssh://evil/system"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := r.Validate(Request{Tool: "virsh", Args: args, ConnectURI: "qemu:///system"})
			if err == nil || !strings.Contains(err.Error(), "managed by the host profile") {
				t.Fatalf("Validate(%q) = %v, want connect command rejection", args, err)
			}
		})
	}
}

func TestValidateAllowsConnectAsCommandArgument(t *testing.T) {
	r := testRunner()
	for _, args := range [][]string{
		{"domname", "connect"},
		{"-d1", "domname", "connect"},
		{"list", "--all"},
		{"start", "vm"},
		{"--", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := r.Validate(Request{Tool: "virsh", Args: args, ConnectURI: "qemu:///system"}); err != nil {
				t.Fatalf("Validate(%q) = %v", args, err)
			}
		})
	}
}

func TestValidateRejectsRemoteQemuImg(t *testing.T) {
	r := testRunner()
	err := r.Validate(Request{Tool: "qemu-img", Args: []string{"info", "x"}, ConnectURI: "qemu+ssh://host/system"})
	if err == nil {
		t.Fatal("expected error for remote qemu-img")
	}
}

func TestValidateRequiresHostURIForHostBoundTools(t *testing.T) {
	r := testRunner()
	for _, req := range []Request{
		{Tool: "virsh", Args: []string{"list"}},
		{Tool: "virsh", Args: []string{"--help"}},
	} {
		if err := r.Validate(req); err == nil || !strings.Contains(err.Error(), "URI is required") {
			t.Fatalf("Validate(%v) = %v, want missing URI rejection", req.Args, err)
		}
	}
}

func TestValidateURI(t *testing.T) {
	valid := []string{
		"qemu:///system",
		"qemu:///session",
		"qemu+ssh://root@example/system",
		"qemu+ssh://root@10.0.0.1:2222/system?keyfile=/etc/wvc/id_ed25519&known_hosts=/etc/wvc/known_hosts",
		"qemu+tcp://libvirt.example/system",
		"qemu+tls://libvirt.example/system?pkipath=/etc/pki/libvirt",
		"qemu+unix:///system?socket=/run/libvirt/libvirt-sock",
	}
	for _, uri := range valid {
		if err := ValidateURI(uri); err != nil {
			t.Errorf("ValidateURI(%q) = %v", uri, err)
		}
	}
	invalid := []string{
		"file:///etc/passwd",
		"xen:///",
		"lxc:///",
		"test:///default",
		"qemu+ssh://-oProxyCommand=x/system",
		"qemu+ssh://-root@example/system",
		"qemu+ssh://root@-example/system",
		"qemu+ssh://root@example:0/system",
		"qemu+ssh://root@example:65536/system",
		"qemu+ssh://root@example/system?foo=bar",
		"qemu+ssh://root@example/system?keyfile=relative",
		"qemu+ssh://root@example/system?keyfile=/a&keyfile=/b",
		"qemu+ssh://root@example/system?keyfile=/a%0a",
		"qemu+ssh://root@bad_host!/system",
		"qemu+tcp://0.0.0.0/system",
		"qemu+ssh://root@example/system#fragment",
		"qemu+ssh://root@example/system?" + strings.Repeat("a", 5000),
		"qemu+ssh://root@example/" + strings.Repeat("a", 5000),
	}
	for _, uri := range invalid {
		if err := ValidateURI(uri); err == nil {
			t.Errorf("ValidateURI(%q) unexpectedly succeeded", uri)
		}
	}
}

func TestRunnerEnvDoesNotLeakApplicationSecrets(t *testing.T) {
	t.Setenv("WVC_DATABASE_URL", "postgres://user:secret@db/wvc")
	t.Setenv("WVC_ADMIN_PASSWORD", "super-secret")
	t.Setenv("WVC_MASTER_KEY_FILE", "/secret/master.key")
	t.Setenv("LIBGUESTFS_BACKEND", "direct")
	t.Setenv("GUESTFS_DEBUG", "1")
	t.Setenv("VIRT_V2V_DEBUG", "1")
	t.Setenv("QEMU_AUDIO_DRV", "none")
	env := runnerEnv()
	for _, item := range env {
		for _, key := range []string{"WVC_DATABASE_URL=", "WVC_ADMIN_PASSWORD=", "WVC_MASTER_KEY_FILE="} {
			if strings.HasPrefix(item, key) {
				t.Fatalf("runner environment leaked %s", item)
			}
		}
	}
	if !containsEnv(env, "LC_ALL=C") || !containsEnv(env, "LANG=C") {
		t.Fatalf("runner environment is missing deterministic locale: %v", env)
	}
	for _, want := range []string{"LIBGUESTFS_BACKEND=direct", "GUESTFS_DEBUG=1", "VIRT_V2V_DEBUG=1", "QEMU_AUDIO_DRV=none"} {
		if !containsEnv(env, want) {
			t.Fatalf("runner environment is missing %s: %v", want, env)
		}
	}
}

func containsEnv(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestRunnerTimeoutKillsProcessGroup(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	r := testRunner()
	r.paths["qemu-img"] = "/bin/sh"
	start := time.Now()
	_, err := r.Run(context.Background(), Request{
		Tool: "qemu-img", Args: []string{"-c", "sleep 30"}, Timeout: 100 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run() error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestClassify(t *testing.T) {
	if ro, known := Classify("virsh", []string{"dominfo", "vm"}); !ro || !known {
		t.Fatal("dominfo should be read-only")
	}
	if ro, _ := Classify("virsh", []string{"destroy", "vm"}); ro {
		t.Fatal("destroy must not be read-only")
	}
	if ro, _ := Classify("qemu-img", []string{"convert", "a", "b"}); ro {
		t.Fatal("convert must not be read-only")
	}
	if ro, known := Classify("virsh", []string{"--readonly", "dumpxml", "vm"}); !ro || !known {
		t.Fatalf("virsh with global options classified as readOnly=%v known=%v", ro, known)
	}
}

func TestCatalogReadOnlyClassification(t *testing.T) {
	for _, item := range Catalog() {
		if item.Tool != "virsh" {
			continue
		}
		ro, known := Classify(item.Tool, []string{item.Command})
		if item.ReadOnly && (!ro || !known) {
			t.Errorf("catalog command %q is read-only but classifier returned readOnly=%v known=%v", item.Command, ro, known)
		}
		if !item.ReadOnly && ro {
			t.Errorf("catalog command %q is read-write but classifier marked it read-only", item.Command)
		}
	}
}

func TestSecretGetValueIsNotReadOnly(t *testing.T) {
	if ro, _ := Classify("virsh", []string{"secret-get-value", "secret"}); ro {
		t.Fatal("secret-get-value must never be classified as read-only")
	}
	if sensitive, _ := Sensitive("virsh", []string{"secret-get-value", "secret"}); !sensitive {
		t.Fatal("secret-get-value must require administrator privileges")
	}
}

func TestCommandLineQuotes(t *testing.T) {
	got := CommandLine("virsh", []string{"snapshot-create-as", "vm", "name with space"})
	if !strings.Contains(got, "'name with space'") {
		t.Fatalf("unexpected command line: %s", got)
	}
}
