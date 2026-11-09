package command

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/config"
)

func findCatalogItem(t *testing.T, tool, name string) CatalogItem {
	t.Helper()
	for _, item := range Catalog() {
		if item.Tool == tool && item.Command == name {
			return item
		}
	}
	t.Fatalf("catalog item %s %s not found", tool, name)
	return CatalogItem{}
}

func hasFlag(specs []ParameterSpec, flag string) bool {
	for _, spec := range specs {
		if spec.Flag == flag {
			return true
		}
	}
	return false
}

func hasAnyFlag(item CatalogItem, flag string) bool {
	return hasFlag(item.Common, flag) || hasFlag(item.Options, flag) || hasFlag(item.Positionals, flag)
}

func hasPositional(specs []ParameterSpec, name string) bool {
	return hasParameter(specs, name)
}

func hasParameter(specs []ParameterSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

func optionByFlag(t *testing.T, specs []ParameterSpec, flag string) ParameterSpec {
	t.Helper()
	for _, spec := range specs {
		if spec.Flag == flag {
			return spec
		}
	}
	t.Fatalf("option %s not found in %#v", flag, specs)
	return ParameterSpec{}
}

func TestCatalogCompleteness(t *testing.T) {
	items := Catalog()
	counts := make(map[string]int)
	for _, item := range items {
		counts[item.Tool]++
		if item.Tool == "virsh" {
			if len(item.ArgPrefix) != 1 || item.ArgPrefix[0] != item.Command {
				t.Errorf("virsh %s has invalid arg prefix %#v", item.Command, item.ArgPrefix)
			}
			if item.DocsURL == "" {
				t.Errorf("virsh %s has invalid docs URL %q", item.Command, item.DocsURL)
			}
		}
	}
	if counts["virsh"] != 284 {
		t.Fatalf("virsh catalog count = %d, want 284 official commands", counts["virsh"])
	}
	if counts["qemu-img"] != 17 {
		t.Fatalf("qemu-img catalog count = %d, want 17", counts["qemu-img"])
	}
	if counts["qemu-io"] != 28 {
		t.Fatalf("qemu-io catalog count = %d, want 28", counts["qemu-io"])
	}
	if counts["qemu-nbd"] != 6 {
		t.Fatalf("qemu-nbd catalog count = %d, want 6", counts["qemu-nbd"])
	}
	for tool, want := range map[string]int{
		"qemu-storage-daemon": 3,
		"qemu-pr-helper":      3,
		"qemu-trace-stap":     2,
		"qemu-vmsr-helper":    3,
		"qemu-vnc":            3,
	} {
		if counts[tool] != want {
			t.Errorf("%s catalog count = %d, want %d", tool, counts[tool], want)
		}
	}
	for tool, want := range map[string]int{
		"virt-admin":                      15,
		"virt-host-validate":              3,
		"virt-qemu-run":                   3,
		"virt-login-shell":                3,
		"virt-xml-validate":               3,
		"virt-pki-validate":               3,
		"virt-sanlock-cleanup":            3,
		"virt-install":                    3,
		"virt-clone":                      3,
		"virt-convert":                    3,
		"virt-xml":                        3,
		"guestfish":                       3,
		"guestmount":                      3,
		"guestunmount":                    3,
		"libguestfs-test-tool":            3,
		"libguestfs-make-fixed-appliance": 3,
		"virt-alignment-scan":             3,
		"virt-builder":                    9,
		"virt-builder-repository":         3,
		"virt-cat":                        3,
		"virt-copy-in":                    3,
		"virt-copy-out":                   3,
		"virt-customize":                  3,
		"virt-df":                         3,
		"virt-dib":                        3,
		"virt-diff":                       3,
		"virt-edit":                       3,
		"virt-filesystems":                3,
		"virt-format":                     3,
		"virt-get-kernel":                 3,
		"virt-inspector":                  3,
		"virt-list-filesystems":           3,
		"virt-list-partitions":            3,
		"virt-log":                        3,
		"virt-ls":                         3,
		"virt-make-fs":                    3,
		"virt-p2v-make-disk":              3,
		"virt-p2v-make-kickstart":         3,
		"virt-p2v":                        3,
		"virt-rescue":                     3,
		"virt-resize":                     3,
		"virt-sparsify":                   3,
		"virt-sysprep":                    3,
		"virt-tail":                       3,
		"virt-tar":                        4,
		"virt-tar-in":                     3,
		"virt-tar-out":                    3,
		"virt-v2v":                        3,
		"virt-v2v-in-place":               3,
		"virt-v2v-inspector":              3,
		"virt-v2v-open":                   3,
		"virt-win-reg":                    3,
		"virt-what":                       3,
		"virt-top":                        3,
	} {
		if counts[tool] != want {
			t.Errorf("%s catalog count = %d, want %d", tool, counts[tool], want)
		}
	}
	if len(items) != 530 {
		t.Fatalf("catalog count = %d, want 530", len(items))
	}
}

func TestVirtToolCatalogCoverage(t *testing.T) {
	items := Catalog()
	seenHelp := make(map[string]bool)
	seenVersion := make(map[string]bool)
	for _, item := range items {
		if item.Tool == "" || item.Category == "" || item.Summary == "" || item.Usage == "" {
			t.Errorf("incomplete virt item: %#v", item)
		}
		switch item.Command {
		case "help":
			seenHelp[item.Tool] = true
		case "version":
			seenVersion[item.Tool] = true
		}
	}
	for _, tool := range config.VirtToolNames {
		if !seenHelp[tool] || !seenVersion[tool] {
			t.Errorf("%s is missing help/version catalog entries", tool)
		}
		if !isVirtCatalogTool(tool) {
			t.Errorf("%s is not classified as a virt catalog tool", tool)
		}
	}
}

func TestVirtToolRegistryMatchesConfiguredTools(t *testing.T) {
	if len(config.VirtToolNames) != 54 {
		t.Fatalf("configured virt tool count = %d, want 54", len(config.VirtToolNames))
	}
	configured := make(map[string]bool, len(config.VirtToolNames))
	for _, tool := range config.VirtToolNames {
		if tool == "" || configured[tool] {
			t.Errorf("configured virt tool %q is empty or duplicated", tool)
		}
		configured[tool] = true
	}
	if len(virtCatalogToolSet) != len(configured) {
		t.Errorf("catalog virt tool count = %d, configured = %d", len(virtCatalogToolSet), len(configured))
	}
	for tool := range configured {
		if !virtCatalogToolSet[tool] {
			t.Errorf("%s is configured but missing from the catalog tool set", tool)
		}
	}
	for tool := range virtCatalogToolSet {
		if !configured[tool] {
			t.Errorf("%s is in the catalog tool set but not configured", tool)
		}
	}
}

func TestVirtCatalogPolicyMatchesMetadata(t *testing.T) {
	for _, item := range Catalog() {
		if !isVirtCatalogTool(item.Tool) {
			continue
		}
		args := append([]string(nil), item.ArgPrefix...)
		if len(args) == 0 {
			args = []string{"--placeholder"}
		}
		readOnly, known := Classify(item.Tool, args)
		if !known || readOnly != item.ReadOnly {
			t.Errorf("%s %s Classify = readOnly=%v known=%v, metadata readOnly=%v", item.Tool, item.Command, readOnly, known, item.ReadOnly)
		}
		sensitive, _ := Sensitive(item.Tool, args)
		if sensitive != item.Sensitive {
			t.Errorf("%s %s Sensitive = %v, metadata %v", item.Tool, item.Command, sensitive, item.Sensitive)
		}
	}
}

func TestCatalogCommandSets(t *testing.T) {
	want := map[string][]string{
		"qemu-img": {
			"amend", "bench", "bitmap", "check", "commit", "compare", "convert", "create", "dd",
			"info", "map", "measure", "rebase", "resize", "snapshot", "help", "version",
		},
		"qemu-io": {
			"abort", "aio_flush", "aio_read", "aio_write", "alloc", "break", "close", "discard",
			"flush", "help", "info", "length", "map", "open", "quit", "read", "readv",
			"remove_break", "reopen", "resume", "sigraise", "sleep", "truncate", "wait_break",
			"write", "writev", "tool-help", "tool-version",
		},
		"qemu-nbd":            {"serve", "list", "connect", "disconnect", "help", "version"},
		"qemu-storage-daemon": {"serve", "help", "version"},
		"qemu-pr-helper":      {"serve", "help", "version"},
		"qemu-trace-stap":     {"list", "run"},
		"qemu-vmsr-helper":    {"serve", "help", "version"},
		"qemu-vnc":            {"serve", "help", "version"},
	}
	got := make(map[string][]string)
	for _, item := range Catalog() {
		got[item.Tool] = append(got[item.Tool], item.Command)
	}
	for tool, commands := range want {
		if strings.Join(got[tool], "\x00") != strings.Join(commands, "\x00") {
			t.Errorf("%s commands = %v, want %v", tool, got[tool], commands)
		}
	}
}

func TestAnalyzeVirshSyntax(t *testing.T) {
	start := findCatalogItem(t, "virsh", "start")
	if !hasFlag(start.Options, "--console") || !hasFlag(start.Options, "--paused") || !hasFlag(start.Options, "--pass-fds") {
		t.Fatalf("start options = %#v", start.Options)
	}
	if !hasPositional(start.Positionals, "domain-name-or-uuid") {
		t.Fatalf("start positionals = %#v", start.Positionals)
	}

	migrate := findCatalogItem(t, "virsh", "migrate")
	required := map[string]bool{}
	for _, spec := range migrate.Positionals {
		required[spec.Name] = spec.Required
	}
	if !required["domain"] || !required["desturi"] || required["migrateuri"] {
		t.Fatalf("migrate positional requirements = %#v", migrate.Positionals)
	}
	if !hasFlag(migrate.Options, "--live") || !hasFlag(migrate.Options, "--postcopy") {
		t.Fatalf("migrate options = %#v", migrate.Options)
	}
	sched := findCatalogItem(t, "virsh", "schedinfo")
	if hasPositional(sched.Positionals, "schedinfo") {
		t.Fatalf("alternate schedinfo synopsis leaked into positionals: %#v", sched.Positionals)
	}

	iftune := findCatalogItem(t, "virsh", "domiftune")
	if !hasFlag(iftune.Options, "--inbound") || !hasFlag(iftune.Options, "--outbound") {
		t.Fatalf("domiftune bandwidth options = %#v", iftune.Options)
	}
	for _, spec := range iftune.Options {
		if spec.Flag == "--inbound" && (spec.Multiple || spec.Placeholder != "average,peak,burst,floor") {
			t.Fatalf("domiftune --inbound spec = %#v", spec)
		}
	}
	for _, spec := range iftune.Positionals {
		if strings.Contains(spec.Name, "*") || strings.HasPrefix(spec.Name, "-") {
			t.Fatalf("domiftune leaked syntax marker into positionals: %#v", iftune.Positionals)
		}
	}
	for _, name := range []string{"domiftune", "pool-define-as"} {
		item := findCatalogItem(t, "virsh", name)
		for _, spec := range item.Positionals {
			if strings.ContainsAny(spec.Name, "*[]{}()|") || strings.HasPrefix(spec.Name, "-") {
				t.Errorf("%s leaked syntax token %q into positionals", name, spec.Name)
			}
		}
	}

	// A comma-separated value is one argv item even when the official syntax
	// writes it as N,M,... or names it disk-list. Only explicitly repeatable
	// options may use the repeated-flag UI.
	passFDs := optionByFlag(t, start.Options, "--pass-fds")
	if passFDs.Multiple || !strings.HasSuffix(passFDs.Placeholder, "...") {
		t.Fatalf("start --pass-fds spec = %#v", passFDs)
	}
	for _, flag := range []string{"--migrate-disks", "--migrate-disks-detect-zeroes", "--migrate-disks-target-zero", "--comp-methods"} {
		spec := optionByFlag(t, migrate.Options, flag)
		if spec.Multiple {
			t.Fatalf("comma/list option %s must not be repeated: %#v", flag, spec)
		}
	}
	for _, name := range []string{"reboot", "shutdown"} {
		item := findCatalogItem(t, "virsh", name)
		if spec := optionByFlag(t, item.Options, "--mode"); spec.Multiple {
			t.Fatalf("%s --mode must be a single comma-separated argument: %#v", name, spec)
		}
	}
}

func TestAnalyzeQemuSyntax(t *testing.T) {
	convert := findCatalogItem(t, "qemu-img", "convert")
	if !hasFlag(convert.Options, "-f") || !hasFlag(convert.Options, "-O") || !hasFlag(convert.Options, "--bitmaps") {
		t.Fatalf("convert options = %#v", convert.Options)
	}
	if !hasPositional(convert.Positionals, "filename") || !hasPositional(convert.Positionals, "output_filename") {
		t.Fatalf("convert positionals = %#v", convert.Positionals)
	}
	for _, spec := range convert.Positionals {
		if spec.Name == "filename2" && (!spec.Multiple || spec.Required) {
			t.Fatalf("convert extra inputs = %#v", spec)
		}
	}
	for _, spec := range convert.Options {
		if spec.Flag == "--object" && !spec.Multiple {
			t.Fatalf("convert --object must be repeatable: %#v", spec)
		}
		if spec.Flag == "-t" && (spec.ValueType != "choice" || len(spec.Choices) == 0) {
			t.Fatalf("convert -t cache choices = %#v", spec)
		}
	}
	bitmap := findCatalogItem(t, "qemu-img", "bitmap")
	if !hasPositional(bitmap.Positionals, "bitmap") {
		t.Fatalf("bitmap name positional missing: %#v", bitmap.Positionals)
	}
	resize := findCatalogItem(t, "qemu-img", "resize")
	for _, spec := range resize.Positionals {
		if spec.Name == "+" || spec.Name == "-" {
			t.Fatalf("resize operators must be part of size: %#v", resize.Positionals)
		}
		if spec.Name == "size" && (!spec.Required || !strings.Contains(spec.Placeholder, "+10G")) {
			t.Fatalf("resize size field = %#v", spec)
		}
	}
	dd := findCatalogItem(t, "qemu-img", "dd")
	prefixes := make(map[string]string)
	for _, spec := range dd.Positionals {
		prefixes[spec.Name] = spec.ValuePrefix
	}
	if prefixes["if"] != "if=" || prefixes["of"] != "of=" || prefixes["bs"] != "bs=" {
		t.Fatalf("qemu-img dd value prefixes = %#v", prefixes)
	}
	for _, spec := range bitmap.Options {
		if (spec.Flag == "--merge" || spec.Flag == "--add") && spec.Required {
			t.Fatalf("mutually exclusive bitmap option %s must not be forced required", spec.Flag)
		}
	}
	check := findCatalogItem(t, "qemu-img", "check")
	if hasPositional(check.Positionals, "all") || hasPositional(check.Positionals, "leaks") {
		t.Fatalf("check repair choices leaked into positionals: %#v", check.Positionals)
	}
	for _, spec := range check.Options {
		if spec.Flag == "-r" && (!spec.OptionalValue || len(spec.Choices) != 2) {
			t.Fatalf("check -r spec = %#v", spec)
		}
	}

	read := findCatalogItem(t, "qemu-io", "read")
	if read.BuildMode != "qemu-io" || !hasFlag(read.Options, "-P") || !hasFlag(read.Options, "-v") {
		t.Fatalf("qemu-io read = %#v", read)
	}
	if !hasPositional(read.Positionals, "off") || !hasPositional(read.Positionals, "len") {
		t.Fatalf("qemu-io read positionals = %#v", read.Positionals)
	}
	if !hasFlag(read.Options, "-p") {
		t.Fatalf("qemu-io read compatibility flag missing: %#v", read.Options)
	}
	write := findCatalogItem(t, "qemu-io", "write")
	if !hasFlag(write.Options, "-p") {
		t.Fatalf("qemu-io write compatibility flag missing: %#v", write.Options)
	}
	sleep := findCatalogItem(t, "qemu-io", "sleep")
	if !hasPositional(sleep.Positionals, "milliseconds") {
		t.Fatalf("qemu-io sleep positionals = %#v", sleep.Positionals)
	}
	for _, spec := range sleep.Positionals {
		if spec.Name == "milliseconds" && (!spec.Required || spec.ValueType != "number") {
			t.Fatalf("qemu-io sleep milliseconds = %#v", spec)
		}
	}
	open := findCatalogItem(t, "qemu-io", "open")
	if !hasFlag(open.Options, "-i") {
		t.Fatalf("qemu-io open AIO option missing: %#v", open.Options)
	}
	if cache := optionByFlag(t, open.Options, "-t"); cache.ValueType != "choice" || len(cache.Choices) == 0 {
		t.Fatalf("qemu-io open cache choices = %#v", cache)
	}
	for _, name := range []string{"aio_read", "aio_write", "readv", "writev"} {
		item := findCatalogItem(t, "qemu-io", name)
		for _, spec := range item.Positionals {
			if spec.Name == "len" && !spec.Multiple {
				t.Fatalf("qemu-io %s len must accept repeated lengths: %#v", name, item.Positionals)
			}
		}
	}
	for _, name := range []string{"break", "remove_break", "resume", "wait_break"} {
		item := findCatalogItem(t, "qemu-io", name)
		if !hasParameter(item.Common, "commands_before") || !hasParameter(item.Common, "commands_after") {
			t.Fatalf("qemu-io %s stateful command sequence fields = %#v", name, item.Common)
		}
	}

	secret := findCatalogItem(t, "virsh", "secret-set-value")
	for _, spec := range secret.Positionals {
		if spec.Name == "base64" || spec.Name == "file" {
			if spec.Required {
				t.Fatalf("alternative secret input %s must not be forced required", spec.Name)
			}
		}
	}
}

func TestAnalyzeVirshOptionalValueForms(t *testing.T) {
	lxc := findCatalogItem(t, "virsh", "lxc-enter-namespace")
	literal := false
	for _, spec := range lxc.Positionals {
		if spec.Literal == "--" && spec.ValueType == "literal" {
			literal = true
		}
	}
	if !literal {
		t.Fatalf("lxc-enter-namespace must keep the -- separator: %#v", lxc.Positionals)
	}
	for _, spec := range lxc.Positionals {
		if spec.Name == "arg1" && spec.Required {
			t.Fatalf("lxc arg1 must be optional: %#v", spec)
		}
		if spec.Name == "arg2" && (spec.Required || !spec.Multiple) {
			t.Fatalf("lxc arg2 must be optional and repeatable: %#v", spec)
		}
	}

	fd := findCatalogItem(t, "virsh", "dom-fd-associate")
	for _, spec := range fd.Positionals {
		if spec.Name == "." || strings.HasPrefix(spec.Name, "-") {
			t.Fatalf("dom-fd-associate leaked syntax punctuation: %#v", fd.Positionals)
		}
	}
	passFDs := optionByFlag(t, fd.Options, "--pass-fds")
	if passFDs.Multiple || passFDs.Placeholder != "M,N,..." || !strings.HasSuffix(passFDs.Label, "...") {
		t.Fatalf("dom-fd-associate --pass-fds = %#v", passFDs)
	}

	stats := findCatalogItem(t, "virsh", "domstats")
	for _, spec := range stats.Positionals {
		if spec.Name == "y" {
			t.Fatalf("domstats leaked upstream typo into positionals: %#v", stats.Positionals)
		}
	}

	alloc := findCatalogItem(t, "virsh", "allocpages")
	for _, flag := range []string{"--pagesize", "--pagecount", "--cellno"} {
		spec := optionByFlag(t, alloc.Options, flag)
		if spec.ValueType == "bool" || spec.Placeholder == "" {
			t.Fatalf("allocpages %s must take a value: %#v", flag, spec)
		}
	}
	for _, spec := range alloc.Positionals {
		if spec.Name == "pagesize" || spec.Name == "pagecount" || spec.Name == "cellno" {
			t.Fatalf("allocpages duplicated option value as positional: %#v", alloc.Positionals)
		}
	}

	freeze := findCatalogItem(t, "virsh", "domfsfreeze")
	mountpoint := optionByFlag(t, freeze.Options, "--mountpoint")
	if mountpoint.ValueType != "text" || !mountpoint.Multiple {
		t.Fatalf("domfsfreeze --mountpoint = %#v", mountpoint)
	}
	if hasPositional(freeze.Positionals, "mountpoint") {
		t.Fatalf("domfsfreeze duplicated mountpoint positional: %#v", freeze.Positionals)
	}

	sched := findCatalogItem(t, "virsh", "schedinfo")
	set := optionByFlag(t, sched.Options, "--set")
	if set.ValueType != "text" || !set.Multiple {
		t.Fatalf("schedinfo --set = %#v", set)
	}
	if hasPositional(sched.Positionals, "parameter") || hasPositional(sched.Positionals, "schedinfo") {
		t.Fatalf("schedinfo leaked alternate syntax into positionals: %#v", sched.Positionals)
	}

	for _, name := range []string{"snapshot-create-as", "checkpoint-create-as"} {
		item := findCatalogItem(t, "virsh", name)
		diskspec := optionByFlag(t, item.Options, "--diskspec")
		if diskspec.ValueType != "text" || !diskspec.Multiple {
			t.Fatalf("%s --diskspec = %#v", name, diskspec)
		}
		if hasPositional(item.Positionals, "diskspec") {
			t.Fatalf("%s duplicated diskspec positional: %#v", name, item.Positionals)
		}
	}

	for _, tc := range []struct{ name, flag, positional string }{
		{"snapshot-list", "--from", "snapshot"},
		{"checkpoint-list", "--from", "checkpoint"},
	} {
		item := findCatalogItem(t, "virsh", tc.name)
		from := optionByFlag(t, item.Options, tc.flag)
		if from.ValueType != "text" || from.Required {
			t.Fatalf("%s --from = %#v", tc.name, from)
		}
		if hasPositional(item.Positionals, tc.positional) {
			t.Fatalf("%s duplicated %s positional: %#v", tc.name, tc.positional, item.Positionals)
		}
	}
}

func TestQemuStandaloneToolsCatalog(t *testing.T) {
	sources := findCatalogItem(t, "virsh", "find-pool-sources-as")
	if sources.Usage != "find-storage-pool-sources-as type [host] [port] [initiator]" {
		t.Fatalf("find-pool-sources-as usage = %q", sources.Usage)
	}
	for _, name := range []string{"find-storage-pool-sources-as"} {
		if hasPositional(sources.Positionals, name) {
			t.Errorf("find-pool-sources-as leaked synopsis command/value token %q: %#v", name, sources.Positionals)
		}
	}
	for _, name := range []string{"type", "host", "port", "initiator"} {
		if !hasPositional(sources.Positionals, name) {
			t.Errorf("find-pool-sources-as missing positional %q: %#v", name, sources.Positionals)
		}
	}
	if readOnly, known := Classify("virsh", []string{"find-pool-sources-as", "iscsi"}); !known || !readOnly {
		t.Fatalf("find-pool-sources-as classification = readOnly=%v known=%v", readOnly, known)
	}
	for _, name := range []string{"cpu-stats", "backup-dumpxml"} {
		if readOnly, known := Classify("virsh", []string{name, "vm"}); !known || !readOnly {
			t.Errorf("Classify(%s) = readOnly=%v known=%v", name, readOnly, known)
		}
	}

	storage := findCatalogItem(t, "qemu-storage-daemon", "serve")
	for _, flag := range []string{"--blockdev", "--chardev", "--export", "--monitor", "--nbd-server", "--object", "--pidfile", "--daemonize", "-T"} {
		if !hasFlag(storage.Options, flag) {
			t.Errorf("qemu-storage-daemon serve missing %s: %#v", flag, storage.Options)
		}
	}
	pr := findCatalogItem(t, "qemu-pr-helper", "serve")
	for _, flag := range []string{"--daemon", "--quiet", "--verbose", "--pidfile", "--socket", "-T", "--user", "--group"} {
		if !hasFlag(pr.Options, flag) {
			t.Errorf("qemu-pr-helper serve missing %s: %#v", flag, pr.Options)
		}
	}
	vmsr := findCatalogItem(t, "qemu-vmsr-helper", "serve")
	if !hasFlag(vmsr.Options, "--socket") || !hasFlag(vmsr.Options, "--user") {
		t.Fatalf("qemu-vmsr-helper serve options = %#v", vmsr.Options)
	}
	format := findCatalogItem(t, "virt-format", "format")
	if hasFlag(format.Common, "--domain") || !hasFlag(format.Common, "--add") || !hasFlag(format.Common, "--format") {
		t.Errorf("virt-format must use disk-image inputs only: %#v", format.Common)
	}
	for _, name := range []string{"virt-list-filesystems", "virt-list-partitions"} {
		item := findCatalogItem(t, name, "list")
		if hasFlag(item.Common, "--add") || hasFlag(item.Common, "--domain") || !hasFlag(item.Common, "--format") {
			t.Errorf("%s common options = %#v, want positional guest/disk input with --format", name, item.Common)
		}
	}
	partitions := findCatalogItem(t, "virt-list-partitions", "list")
	if !hasFlag(partitions.Options, "--total") {
		t.Errorf("virt-list-partitions missing --total: %#v", partitions.Options)
	}
	vnc := findCatalogItem(t, "qemu-vnc", "serve")
	for _, flag := range []string{
		"--dbus-address", "--dbus-p2p-fd", "--bus-name", "--wait", "--password", "--vnc-addr",
		"--websocket", "--object", "--tls-creds", "--tls-authz", "--sasl", "--sasl-authz",
		"--share", "--vt-chardev", "--no-vt", "--keyboard-layout", "--lossy", "--non-adaptive", "-T",
	} {
		if !hasFlag(vnc.Options, flag) {
			t.Errorf("qemu-vnc serve missing %s: %#v", flag, vnc.Options)
		}
	}

	list := findCatalogItem(t, "qemu-trace-stap", "list")
	if len(list.Common) != 1 || list.Common[0].Flag != "-v" || len(list.ArgPrefix) != 1 || list.ArgPrefix[0] != "list" {
		t.Fatalf("qemu-trace-stap list shape = %#v", list)
	}
	run := findCatalogItem(t, "qemu-trace-stap", "run")
	if !hasFlag(run.Options, "--pid") || !hasPositional(run.Positionals, "pattern") {
		t.Fatalf("qemu-trace-stap run shape = %#v", run)
	}

	imgHelp := findCatalogItem(t, "qemu-img", "help")
	if len(imgHelp.ArgPrefix) != 1 || imgHelp.ArgPrefix[0] != "--help" {
		t.Fatalf("qemu-img help shape = %#v", imgHelp)
	}
	ioHelp := findCatalogItem(t, "qemu-io", "tool-help")
	if len(ioHelp.ArgPrefix) != 1 || ioHelp.ArgPrefix[0] != "--help" || ioHelp.BuildMode != "" {
		t.Fatalf("qemu-io tool-help shape = %#v", ioHelp)
	}
}

func TestQemuNbdCommandForms(t *testing.T) {
	list := findCatalogItem(t, "qemu-nbd", "list")
	if !hasFlag(list.Options, "--tls-hostname") || hasFlag(list.Options, "--handshake-limit") || !hasFlag(list.Options, "--socket") {
		t.Fatalf("qemu-nbd list options = %#v", list.Options)
	}
	connect := findCatalogItem(t, "qemu-nbd", "connect")
	for _, flag := range []string{"--connect", "--cache", "--aio", "--discard", "--detect-zeroes", "--image-opts"} {
		if !hasFlag(connect.Options, flag) {
			t.Fatalf("qemu-nbd connect missing %s: %#v", flag, connect.Options)
		}
	}
	disconnect := findCatalogItem(t, "qemu-nbd", "disconnect")
	if len(disconnect.Options) != 0 {
		t.Fatalf("qemu-nbd disconnect takes no image options: %#v", disconnect.Options)
	}
	serve := findCatalogItem(t, "qemu-nbd", "serve")
	if !hasFlag(serve.Options, "--handshake-limit") {
		t.Fatalf("qemu-nbd serve missing --handshake-limit: %#v", serve.Options)
	}
}

func TestCatalogMarksParametersRejectedByAsyncMode(t *testing.T) {
	for _, tc := range []struct {
		tool, command, flag string
	}{
		{"qemu-storage-daemon", "serve", "--daemonize"},
		{"qemu-pr-helper", "serve", "--daemon"},
		{"qemu-vmsr-helper", "serve", "--daemon"},
		{"qemu-nbd", "serve", "--fork"},
		{"guestfish", "execute", "--listen"},
		{"virt-builder-repository", "update", "--interactive"},
	} {
		item := findCatalogItem(t, tc.tool, tc.command)
		spec := optionByFlag(t, item.Options, tc.flag)
		if !spec.Disabled || spec.DisabledReason == "" {
			t.Errorf("%s %s %s is not marked disabled: %#v", tc.tool, tc.command, tc.flag, spec)
		}
	}
	edit := findCatalogItem(t, "virt-edit", "edit")
	expr := optionByFlag(t, edit.Options, "--expr")
	if !expr.Required || expr.Disabled {
		t.Fatalf("virt-edit --expr must be required and enabled: %#v", expr)
	}
	guestmount := findCatalogItem(t, "guestmount", "mount")
	if !guestmount.Disabled || guestmount.DisabledReason == "" {
		t.Fatalf("guestmount must be disabled for asynchronous jobs: %#v", guestmount)
	}
	top := findCatalogItem(t, "virt-top", "batch")
	for _, flag := range []string{"-b", "-n"} {
		spec := optionByFlag(t, top.Options, flag)
		if !spec.Required || spec.Disabled {
			t.Errorf("virt-top %s must be required and enabled: %#v", flag, spec)
		}
	}
}

func TestCatalogClassificationUsesCompleteMetadata(t *testing.T) {
	for _, name := range []string{"domif-getlink", "guestvcpus", "checkpoint-list", "net-port-dumpxml", "pool-name"} {
		readOnly, known := Classify("virsh", []string{name})
		if !known || !readOnly {
			t.Errorf("Classify(%s) = readOnly=%v known=%v", name, readOnly, known)
		}
	}
	for _, name := range []string{"domjobabort", "domdisplay-reload", "set-user-password", "nwfilter-binding-create"} {
		readOnly, known := Classify("virsh", []string{name})
		if !known || readOnly {
			t.Errorf("Classify(%s) = readOnly=%v known=%v", name, readOnly, known)
		}
	}
	if sensitive, _ := Sensitive("virsh", []string{"get-user-sshkeys", "vm"}); !sensitive {
		t.Fatal("get-user-sshkeys must be sensitive")
	}
	if readOnly, known := Classify("qemu-img", []string{"bench", "image.qcow2"}); readOnly || !known {
		t.Fatalf("qemu-img bench classification = readOnly=%v known=%v", readOnly, known)
	}
}

func TestVirtCatalogPolicyTreatsHostFileOutputAsWrite(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tool     string
		args     []string
		readOnly bool
	}{
		{name: "virt-top csv", tool: "virt-top", args: []string{"--script", "--csv", "/tmp/top.csv"}, readOnly: false},
		{name: "virt-top debug", tool: "virt-top", args: []string{"--script", "--debug=/tmp/top.log"}, readOnly: false},
		{name: "virt-top stdout only", tool: "virt-top", args: []string{"--script", "--stream"}, readOnly: true},
		{name: "virt-v2v-inspector output", tool: "virt-v2v-inspector", args: []string{"-O", "/tmp/inspect.xml", "guest"}, readOnly: false},
		{name: "virt-v2v-inspector attached output", tool: "virt-v2v-inspector", args: []string{"-O/tmp/inspect.xml", "guest"}, readOnly: false},
		{name: "virt-v2v-inspector stdout", tool: "virt-v2v-inspector", args: []string{"guest"}, readOnly: true},
	} {
		readOnly, known := Classify(tc.tool, tc.args)
		if !known || readOnly != tc.readOnly {
			t.Errorf("%s: Classify(%s, %v) = readOnly=%v known=%v, want readOnly=%v known=true",
				tc.name, tc.tool, tc.args, readOnly, known, tc.readOnly)
		}
	}
}

func TestCatalogSensitiveMetadataMatchesPolicy(t *testing.T) {
	for _, item := range Catalog() {
		if item.Tool != "virsh" {
			continue
		}
		sensitive, _ := Sensitive(item.Tool, []string{item.Command})
		if sensitive != item.Sensitive {
			t.Errorf("virsh %s metadata sensitive=%v policy=%v", item.Command, item.Sensitive, sensitive)
		}
	}
}

func TestQemuHelperInformationalInvocationsAreNotSensitive(t *testing.T) {
	for _, tool := range []string{"qemu-storage-daemon", "qemu-pr-helper", "qemu-vmsr-helper", "qemu-vnc"} {
		for _, args := range [][]string{{"--help"}, {"--version"}} {
			if sensitive, reason := Sensitive(tool, args); sensitive {
				t.Errorf("Sensitive(%s, %v) = true (%s), want false", tool, args, reason)
			}
		}
	}
}

func TestCatalogOmitsOptionsRejectedByCurrentTools(t *testing.T) {
	for _, tc := range []struct {
		tool, command, flag string
	}{
		{"virt-install", "create", "--attach"},
		{"virt-xml-validate", "validate", "--quiet"},
		{"virt-top", "batch", "--batch"},
		{"virt-tar-in", "extract", "--compress"},
		{"virt-tar-out", "archive", "--compress"},
		{"virt-sparsify", "sparsify", "--output-format"},
		{"virt-sparsify", "sparsify", "--no-check-tmpdir"},
		{"virt-customize", "customize", "--machine-readable"},
		{"virt-customize", "customize", "--trace"},
		{"virt-sysprep", "run", "--machine-readable"},
		{"virt-sysprep", "run", "--trace"},
		{"virt-sysprep", "run", "--debug"},
		{"virt-p2v-make-disk", "build", "--format"},
		{"virt-p2v-make-disk", "build", "--prefix"},
		{"virt-p2v-make-disk", "build", "--force"},
	} {
		item := findCatalogItem(t, tc.tool, tc.command)
		if hasAnyFlag(item, tc.flag) {
			t.Errorf("%s %s still exposes unsupported option %s", tc.tool, tc.command, tc.flag)
		}
	}
	for _, tc := range []struct {
		tool, command, flag string
	}{
		{"virt-top", "batch", "-b"},
		{"virt-sparsify", "sparsify", "--check-tmpdir"},
		{"virt-customize", "customize", "-x"},
		{"virt-sysprep", "run", "-x"},
		{"virt-p2v-make-disk", "build", "--inject-ssh-identity"},
		{"virt-p2v-make-kickstart", "build", "--scratch"},
	} {
		item := findCatalogItem(t, tc.tool, tc.command)
		if !hasAnyFlag(item, tc.flag) {
			t.Errorf("%s %s missing supported option %s", tc.tool, tc.command, tc.flag)
		}
	}
}

func TestCatalogSafetyRequirementsAreRepresentable(t *testing.T) {
	edit := findCatalogItem(t, "virt-edit", "edit")
	if !optionByFlag(t, edit.Options, "--expr").Required {
		t.Fatal("virt-edit --expr must be marked required in the form metadata")
	}

	top := findCatalogItem(t, "virt-top", "batch")
	if len(top.ArgPrefix) != 0 {
		t.Fatalf("virt-top should not inject an extra mode argument: %#v", top.ArgPrefix)
	}
	if !optionByFlag(t, top.Options, "-b").Required {
		t.Fatal("virt-top -b must be marked required")
	}
	if !optionByFlag(t, top.Options, "-n").Required {
		t.Fatal("virt-top -n must be marked required")
	}
	if err := ValidateAsyncMode("virt-top", []string{"-b", "-n", "1"}); err != nil {
		t.Fatalf("represented virt-top form failed async validation: %v", err)
	}
	if err := ValidateAsyncMode("virt-edit", []string{"--expr", "s/a/b/", "-d", "vm01", "/etc/hosts"}); err != nil {
		t.Fatalf("represented virt-edit form failed async validation: %v", err)
	}
}

func TestCatalogStructure(t *testing.T) {
	seen := make(map[string]bool)
	for _, item := range Catalog() {
		key := item.Tool + "\x00" + item.Command
		if seen[key] {
			t.Errorf("duplicate catalog item %s %s", item.Tool, item.Command)
		}
		seen[key] = true
		if item.Tool == "" || item.Command == "" || item.Category == "" {
			t.Errorf("incomplete catalog item: %#v", item)
		}
		for _, specs := range [][]ParameterSpec{item.Common, item.Options, item.Positionals} {
			flags := make(map[string]bool)
			for _, spec := range specs {
				if spec.Flag != "" {
					if !strings.HasPrefix(spec.Flag, "-") || strings.ContainsAny(spec.Flag, " \t\r\n") {
						t.Errorf("%s %s has invalid flag %q", item.Tool, item.Command, spec.Flag)
					}
					if flags[spec.Flag] {
						t.Errorf("%s %s has duplicate flag %q", item.Tool, item.Command, spec.Flag)
					}
					flags[spec.Flag] = true
				}
				if spec.Literal != "" {
					continue
				}
				if spec.Flag == "" && (spec.Name == "" || strings.HasPrefix(spec.Name, "-") || strings.ContainsAny(spec.Name, "*[]{}()|,")) {
					t.Errorf("%s %s has invalid parameter name %q", item.Tool, item.Command, spec.Name)
				}
				switch spec.ValueType {
				case "bool", "text", "number", "choice", "textarea":
				default:
					t.Errorf("%s %s has unknown value type %q", item.Tool, item.Command, spec.ValueType)
				}
				if spec.ValueType == "choice" && len(spec.Choices) == 0 {
					t.Errorf("%s %s choice parameter %q has no choices", item.Tool, item.Command, spec.Name)
				}
			}
		}
	}
}

func TestDiscoveryHelpers(t *testing.T) {
	groups := parseVirshGroups("Group of commands:\n\n    domain    Domain commands\n    host      Host commands\n")
	if len(groups) != 2 || groups[0] != "domain" || groups[1] != "host" {
		t.Fatalf("groups = %#v", groups)
	}
	known := map[string]bool{"start": true, "list": true}
	commands := parseVirshCommandHelp(" Domain Commands:\n    start    start a domain\n    unrelated    ignore me\n", known)
	if len(commands) != 1 || !commands["start"] {
		t.Fatalf("commands = %#v", commands)
	}
	items := ApplyDiscovery([]CatalogItem{{Tool: "virsh", Command: "start"}, {Tool: "virsh", Command: "old"}}, Discovery{Tools: map[string]ToolCapability{
		"virsh": {Name: "virsh", Commands: map[string]bool{"start": true}},
	}})
	if items[0].ProbeState != "advertised" || items[1].ProbeState != "missing" {
		t.Fatalf("probe states = %q, %q", items[0].ProbeState, items[1].ProbeState)
	}
}

func TestQemuImgCheckRepairIsTreatedAsWrite(t *testing.T) {
	for _, args := range [][]string{
		{"check", "disk.qcow2"},
		{"check", "-r", "disk.qcow2"},
		{"check", "-r", "all", "disk.qcow2"},
		{"check", "--repair", "leaks", "disk.qcow2"},
	} {
		readOnly, known := Classify("qemu-img", args)
		if !known || readOnly {
			t.Errorf("Classify(qemu-img, %v) = readOnly=%v known=%v, want readOnly=false known=true", args, readOnly, known)
		}
	}
}
