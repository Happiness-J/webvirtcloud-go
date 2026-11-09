package command

import "testing"

func TestQemuImgGlobalOptionsDoNotHideSubcommand(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		readOnly bool
		known    bool
	}{
		{name: "trace separate value", args: []string{"-T", "enable=info", "info", "disk.qcow2"}, readOnly: true, known: true},
		{name: "trace attached value", args: []string{"-Tenable=info", "map", "disk.qcow2"}, readOnly: true, known: true},
		{name: "trace long equals", args: []string{"--trace=enable=info", "compare", "a", "b"}, readOnly: true, known: true},
		{name: "trace long separate", args: []string{"--trace", "enable=info", "measure", "disk.qcow2"}, readOnly: true, known: true},
		{name: "help behind trace", args: []string{"-T", "enable=info", "--help"}, readOnly: true, known: true},
		{name: "write behind trace", args: []string{"-T", "enable=info", "convert", "a", "b"}, readOnly: false, known: true},
		{name: "create behind trace", args: []string{"--trace=enable=info", "create", "disk.qcow2", "1G"}, readOnly: false, known: true},
		{name: "unknown global option", args: []string{"--not-a-qemu-img-option", "info", "disk.qcow2"}, readOnly: false, known: false},
		{name: "missing trace value", args: []string{"-T"}, readOnly: false, known: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readOnly, known := Classify("qemu-img", tc.args)
			if readOnly != tc.readOnly || known != tc.known {
				t.Fatalf("Classify(qemu-img, %v) = readOnly=%v known=%v, want readOnly=%v known=%v", tc.args, readOnly, known, tc.readOnly, tc.known)
			}
		})
	}
}

func TestQemuNbdListModeWithOtherOptions(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		readOnly bool
		known    bool
	}{
		{name: "format before list", args: []string{"--format", "qcow2", "--list"}, readOnly: true, known: true},
		{name: "short format before list", args: []string{"-f", "qcow2", "-L"}, readOnly: true, known: true},
		{name: "list before format", args: []string{"-L", "--format", "qcow2"}, readOnly: true, known: true},
		{name: "long prefix", args: []string{"--li"}, readOnly: true, known: true},
		{name: "list consumed as description", args: []string{"--description", "--list"}, readOnly: false, known: true},
		{name: "list consumed as format", args: []string{"--format", "--list"}, readOnly: false, known: true},
		{name: "tls hostname before list", args: []string{"--tls-hostname", "nbd.example", "--list"}, readOnly: true, known: true},
		{name: "list consumed as tls hostname", args: []string{"--tls-hostname", "--list"}, readOnly: false, known: true},
		{name: "handshake limit before list", args: []string{"--handshake-limit", "30", "--list"}, readOnly: true, known: true},
		{name: "list consumed as handshake limit", args: []string{"--handshake-limit", "--list"}, readOnly: false, known: true},
		{name: "help behind options", args: []string{"--format", "qcow2", "--help"}, readOnly: true, known: true},
		{name: "serve", args: []string{"--format", "qcow2", "--port", "10809", "disk.qcow2"}, readOnly: false, known: true},
		{name: "unknown long option", args: []string{"--not-an-nbd-option", "-L"}, readOnly: false, known: false},
		{name: "unknown short option", args: []string{"-Z", "-L"}, readOnly: false, known: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readOnly, known := Classify("qemu-nbd", tc.args)
			if readOnly != tc.readOnly || known != tc.known {
				t.Fatalf("Classify(qemu-nbd, %v) = readOnly=%v known=%v, want readOnly=%v known=%v", tc.args, readOnly, known, tc.readOnly, tc.known)
			}
		})
	}
}

func TestQemuTraceSTAPGlobalVerboseOption(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		readOnly bool
		known    bool
	}{
		{args: []string{"-v", "list", "qemu-system-x86_64", "qio*"}, readOnly: true, known: true},
		{args: []string{"-v", "run", "qemu-system-x86_64", "qio*"}, readOnly: false, known: true},
	} {
		readOnly, known := Classify("qemu-trace-stap", tc.args)
		if readOnly != tc.readOnly || known != tc.known {
			t.Errorf("Classify(qemu-trace-stap, %v) = readOnly=%v known=%v, want readOnly=%v known=%v", tc.args, readOnly, known, tc.readOnly, tc.known)
		}
	}
}

func TestQemuSensitiveRecognizesGlobalInformationalOptions(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args []string
	}{
		{tool: "qemu-img", args: []string{"-T", "enable=info", "--help"}},
		{tool: "qemu-nbd", args: []string{"--format", "qcow2", "--version"}},
		{tool: "qemu-trace-stap", args: []string{"-v", "list", "qemu-system-x86_64"}},
	} {
		if sensitive, reason := Sensitive(tc.tool, tc.args); sensitive {
			t.Errorf("Sensitive(%s, %v) = true (%s), want false", tc.tool, tc.args, reason)
		}
	}
}
