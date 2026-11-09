package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQemuToolDiscoveryIncludesHelpEntries(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "qemu-img"), `#!/bin/sh
printf 'Command syntax:\n  info [--object objectdef] filename\n'
`)
	writeExecutable(t, filepath.Join(dir, "qemu-io"), `#!/bin/sh
printf 'read [-abCqv] off len -- reads a number of bytes\n'
`)

	runner := &Runner{paths: map[string]string{
		"qemu-img": filepath.Join(dir, "qemu-img"),
		"qemu-io":  filepath.Join(dir, "qemu-io"),
	}}

	imgCommands, errText := probeQemuImgCommands(context.Background(), runner)
	if errText != "" {
		t.Fatalf("probe qemu-img: %s", errText)
	}
	for _, name := range []string{"help", "version", "info"} {
		if !imgCommands[name] {
			t.Errorf("qemu-img discovery missing %q: %#v", name, imgCommands)
		}
	}

	ioCommands, errText := probeQemuIOCommands(context.Background(), runner)
	if errText != "" {
		t.Fatalf("probe qemu-io: %s", errText)
	}
	for _, name := range []string{"tool-help", "tool-version", "read"} {
		if !ioCommands[name] {
			t.Errorf("qemu-io discovery missing %q: %#v", name, ioCommands)
		}
	}
}

func TestQemuImgDiscoverySupportsRecognizedCommandsHeading(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "qemu-img"), `#!/bin/sh
printf 'Recognized commands (run qemu-img COMMAND --help for command-specific help):\n\n  info - Display information about the image\n  convert - Copy one or more images\n'
`)
	runner := &Runner{paths: map[string]string{"qemu-img": filepath.Join(dir, "qemu-img")}}
	commands, errText := probeQemuImgCommands(context.Background(), runner)
	if errText != "" {
		t.Fatal(errText)
	}
	for _, name := range []string{"info", "convert"} {
		if !commands[name] {
			t.Fatalf("qemu-img discovery missing %q: %#v", name, commands)
		}
	}
}

func TestParseVirshGroupsSupportsGroupedCommandsHeading(t *testing.T) {
	output := `Grouped commands:

 Domain Management (help keyword 'domain'):
    start  start a domain

 Storage Management (help keyword 'storage'):
    pool-list  list pools
`
	groups := parseVirshGroups(output)
	if len(groups) != 2 || groups[0] != "domain" || groups[1] != "storage" {
		t.Fatalf("parseVirshGroups() = %#v", groups)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverProbesToolsConcurrently(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-virt-tool")
	writeExecutable(t, script, `#!/bin/sh
sleep 0.6
printf 'test version\n'
`)
	names := []string{"virt-cat", "virt-df", "virt-diff", "virt-ls", "virt-log", "virt-format"}
	runner := &Runner{
		paths: make(map[string]string, len(names)),
		tools: make([]Tool, 0, len(names)),
	}
	for _, name := range names {
		runner.paths[name] = script
		runner.tools = append(runner.tools, Tool{Name: name, Path: script, Available: true})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	start := time.Now()
	discovery := Discover(ctx, runner)
	elapsed := time.Since(start)
	for _, name := range names {
		capability := discovery.Tools[name]
		if capability.Version != "test version" {
			t.Errorf("%s version = %q, want test version (error %q)", name, capability.Version, capability.Error)
		}
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("discovery took %s; version probes are still serialized", elapsed)
	}
}

func TestDiscoverToolUsesCatalogForGuestfsHelpers(t *testing.T) {
	r := testRunner()
	for _, name := range []string{
		"guestfish", "guestmount", "guestunmount",
		"libguestfs-test-tool", "libguestfs-make-fixed-appliance",
	} {
		r.paths[name] = "/bin/true"
		capability := discoverTool(context.Background(), r, Tool{Name: name, Available: true})
		if len(capability.Commands) == 0 {
			t.Errorf("%s discovery returned no catalog commands", name)
		}
	}
}
