package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/remote"
)

type Runner struct {
	paths          map[string]string
	tools          []Tool
	sshPath        string
	remotePATH     string
	maxOutputBytes int
	maxArgs        int
	maxStdinBytes  int
	defaultTimeout time.Duration
	maxTimeout     time.Duration
}

func NewRunner(cfg config.Config) *Runner {
	defs := []struct {
		name     string
		path     string
		purpose  string
		required bool
	}{
		{"virsh", cfg.VirshBin, "libvirt 管理命令行", true},
		{"qemu-img", cfg.QemuImgBin, "QEMU 磁盘镜像工具（本机运行）", true},
		{"qemu-io", cfg.QemuIOBin, "QEMU 块设备 I/O 工具（本机运行）", true},
		{"qemu-nbd", cfg.QemuNbdBin, "QEMU NBD 服务/检查工具（本机运行）", true},
		{"qemu-storage-daemon", cfg.QemuStorageDaemonBin, "QEMU 独立存储守护进程（本机运行）", false},
		{"qemu-pr-helper", cfg.QemuPRHelperBin, "QEMU SCSI 持久保留 helper（本机运行）", false},
		{"qemu-trace-stap", cfg.QemuTraceStapBin, "QEMU SystemTap 跟踪工具（本机运行）", false},
		{"qemu-vmsr-helper", cfg.QemuVMSRHelperBin, "QEMU RAPL MSR helper（本机运行）", false},
		{"qemu-vnc", cfg.QemuVNCBin, "QEMU 独立 VNC 服务（本机运行）", false},
	}
	r := &Runner{
		paths:          make(map[string]string, len(defs)),
		remotePATH:     cfg.RemotePATH,
		maxOutputBytes: cfg.MaxOutputBytes,
		maxArgs:        cfg.MaxArgs,
		maxStdinBytes:  cfg.MaxStdinBytes,
		defaultTimeout: cfg.CommandTimeout,
		maxTimeout:     cfg.MaxTimeout,
	}
	if r.remotePATH == "" {
		r.remotePATH = config.DefaultRemotePATH
	}
	if sshPath, err := exec.LookPath(cfg.SSHBin); err == nil {
		r.sshPath = sshPath
	}
	for _, d := range defs {
		path, err := exec.LookPath(d.path)
		available := err == nil
		if available {
			r.paths[d.name] = path
		}
		r.tools = append(r.tools, Tool{Name: d.name, Path: path, Available: available, Required: d.required, Purpose: d.purpose})
	}
	for _, name := range config.VirtToolNames {
		configured := cfg.VirtBins[name]
		if configured == "" {
			configured = name
		}
		path, err := exec.LookPath(configured)
		available := err == nil
		if available {
			r.paths[name] = path
		}
		r.tools = append(r.tools, Tool{
			Name: name, Path: configured, Available: available, Required: false,
			Purpose: virtToolPurpose(name),
		})
	}
	return r
}

func virtToolPurpose(name string) string {
	switch {
	case name == "virt-install" || name == "virt-clone" || name == "virt-convert" || name == "virt-xml":
		return "virtinst：创建、克隆和编辑 libvirt 虚拟机"
	case name == "virt-admin" || name == "virt-host-validate" || name == "virt-qemu-run" ||
		name == "virt-login-shell" || name == "virt-xml-validate" ||
		name == "virt-pki-validate" || name == "virt-sanlock-cleanup":
		return "libvirt virt 辅助工具"
	case strings.HasPrefix(name, "virt-p2v"):
		return "virt-p2v：生成物理机迁移介质"
	case name == "virt-v2v" || strings.HasPrefix(name, "virt-v2v-"):
		return "virt-v2v：异构虚拟机迁移"
	case name == "virt-what":
		return "检测当前宿主机的虚拟化环境"
	case name == "virt-top":
		return "libvirt 资源监控（批处理模式）"
	case strings.HasPrefix(name, "virt-"), strings.HasPrefix(name, "libguestfs-"),
		name == "guestfish", name == "guestmount", name == "guestunmount":
		return "libguestfs：离线检查、修改和转换虚拟机磁盘"
	default:
		return "virt 工具"
	}
}

func (r *Runner) Tools() []Tool { return append([]Tool(nil), r.tools...) }

func (r *Runner) ToolAvailable(tool string) bool {
	_, ok := r.paths[tool]
	return ok
}

// SSHAvailable reports whether remote qemu+ssh targets can be reached through
// the configured OpenSSH client. Local-only host profiles do not depend on it.
func (r *Runner) SSHAvailable() bool { return r.sshPath != "" }

func ValidateURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("libvirt URI is required")
	}
	if len(uri) > 4096 {
		return fmt.Errorf("libvirt URI is too long")
	}
	if strings.ContainsAny(uri, "\x00\r\n\t") || strings.Contains(uri, "\\") {
		return fmt.Errorf("libvirt URI contains control characters")
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("invalid libvirt URI")
	}
	if u.Fragment != "" {
		return fmt.Errorf("libvirt URI fragments are not allowed")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("libvirt URI contains a malformed query")
	}
	if u.User != nil {
		if password, set := u.User.Password(); set || password != "" {
			return fmt.Errorf("passwords in libvirt URIs are not allowed; use SSH keys or libvirt auth files")
		}
	}
	allowedSchemes := map[string]bool{
		"qemu": true, "qemu+ssh": true, "qemu+unix": true, "qemu+tcp": true, "qemu+tls": true,
	}
	if !allowedSchemes[u.Scheme] {
		return fmt.Errorf("unsupported libvirt URI scheme: %s", u.Scheme)
	}
	if host := u.Hostname(); host != "" {
		if err := validateURIHost(host); err != nil {
			return err
		}
	}
	if u.User != nil {
		username := u.User.Username()
		if username != "" && !validSSHUsername(username) {
			return fmt.Errorf("libvirt URI username contains invalid characters")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("libvirt URI has an empty port")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("libvirt URI port must be between 1 and 65535")
		}
	}
	path := u.EscapedPath()
	switch u.Scheme {
	case "qemu":
		if u.Host != "" || u.User != nil {
			return fmt.Errorf("local qemu URI must not contain a host")
		}
		if path != "/system" && path != "/session" {
			return fmt.Errorf("local qemu URI path must be /system or /session")
		}
	case "qemu+ssh":
		if u.Hostname() == "" || (net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsUnspecified()) {
			return fmt.Errorf("qemu+ssh URI requires a hostname")
		}
		if path != "/system" && path != "/session" {
			return fmt.Errorf("qemu+ssh URI path must be /system or /session")
		}
	case "qemu+unix":
		if u.Host != "" || u.User != nil {
			return fmt.Errorf("qemu+unix URI must not contain a host")
		}
		if path != "/system" && path != "/session" {
			return fmt.Errorf("qemu+unix URI path must be /system or /session")
		}
	case "qemu+tcp", "qemu+tls":
		if u.Hostname() == "" {
			return fmt.Errorf("%s URI requires a hostname", u.Scheme)
		}
		if path != "/system" && path != "/session" {
			return fmt.Errorf("%s URI path must be /system or /session", u.Scheme)
		}
	}
	allowedQuery := map[string]bool{
		"keyfile": true, "known_hosts": true, "knownhosts": true, "socket": true,
		"pkipath": true, "tls_priority": true,
	}
	for key, values := range query {
		if !allowedQuery[key] {
			return fmt.Errorf("unsupported libvirt URI parameter %q", key)
		}
		if len(values) != 1 {
			return fmt.Errorf("libvirt URI parameter %q must appear once", key)
		}
		rawValue := values[0]
		value := strings.TrimSpace(rawValue)
		if value == "" || len(value) > 4096 || containsControl(rawValue) {
			return fmt.Errorf("libvirt URI parameter %q is invalid", key)
		}
		if key == "keyfile" || key == "socket" || key == "pkipath" || key == "known_hosts" || key == "knownhosts" {
			if !filepath.IsAbs(value) {
				return fmt.Errorf("libvirt URI parameter %q must be an absolute path", key)
			}
		}
	}
	return nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validateURIHost(host string) error {
	if host == "" || len(host) > 253 || !utf8.ValidString(host) {
		return fmt.Errorf("libvirt URI hostname is invalid")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return fmt.Errorf("libvirt URI must not use an unspecified host address")
		}
		return nil
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return fmt.Errorf("libvirt URI hostname is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("libvirt URI hostname contains an invalid label")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				continue
			}
			return fmt.Errorf("libvirt URI hostname contains invalid characters")
		}
	}
	return nil
}

func validSSHUsername(username string) bool {
	if username == "" || len(username) > 64 || username[0] == '-' || username[0] == '.' {
		return false
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (r *Runner) Validate(req Request) error {
	if _, known := toolNames[req.Tool]; !known {
		return fmt.Errorf("%w: %s", ErrUnknownTool, req.Tool)
	}
	if req.SSH == nil {
		if _, ok := r.paths[req.Tool]; !ok {
			return fmt.Errorf("%w: %s", ErrToolMissing, req.Tool)
		}
	} else {
		if r.sshPath == "" {
			return fmt.Errorf("%w: %s", ErrToolMissing, "ssh")
		}
		if err := req.SSH.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidArgs, err)
		}
	}
	if len(req.Args) == 0 && !AllowsEmptyArgs(req.Tool) {
		return fmt.Errorf("%w: at least one argument is required", ErrInvalidArgs)
	}
	if len(req.Args) > r.maxArgs {
		return fmt.Errorf("%w: too many arguments (max %d)", ErrInvalidArgs, r.maxArgs)
	}
	total := 0
	for _, a := range req.Args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%w: NUL byte in argument", ErrInvalidArgs)
		}
		total += len(a)
		if len(a) > 4096 {
			return fmt.Errorf("%w: argument is too long", ErrInvalidArgs)
		}
	}
	if total > 32*1024 {
		return fmt.Errorf("%w: arguments are too large", ErrInvalidArgs)
	}
	if len(req.Stdin) > r.maxStdinBytes {
		return fmt.Errorf("%w: stdin is too large", ErrInvalidArgs)
	}
	if UsesHostURI(req.Tool) {
		if req.Tool != "virsh" {
			if err := rejectConnectionOverride(req.Tool, req.Args); err != nil {
				return err
			}
		}
		if strings.TrimSpace(req.ConnectURI) == "" {
			return fmt.Errorf("%w: a libvirt connection URI is required for %s", ErrInvalidArgs, req.Tool)
		}
		if err := ValidateURI(req.ConnectURI); err != nil {
			return err
		}
		if req.ConnectURI != "" && strings.ContainsAny(req.ConnectURI, " \t\r\n\x00") {
			return fmt.Errorf("%w: invalid character in connection URI", ErrInvalidArgs)
		}
	}
	if req.Tool == "virsh" {
		for _, a := range req.Args {
			// GNU getopt accepts unambiguous long-option prefixes, so --con and
			// --conn can select --connect just as effectively as the full name.
			if a == "--" {
				break
			}
			longName := ""
			if strings.HasPrefix(a, "--") {
				longName, _, _ = strings.Cut(strings.TrimPrefix(a, "--"), "=")
			}
			if a == "-c" || strings.HasPrefix(a, "-c") || (longName != "" && strings.HasPrefix("connect", longName)) {
				return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
			}
			if strings.ContainsAny(a, "\r\n\x00") {
				return fmt.Errorf("%w: control character in argument", ErrInvalidArgs)
			}
		}
		if virshCommandOverridesConnection(req.Args) {
			return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
		}
	}
	if !UsesHostURI(req.Tool) && req.ConnectURI != "" {
		return fmt.Errorf("%w: %s 不接受 libvirt 连接 URI", ErrNotAllowed, req.Tool)
	}
	if blocked, reason := Blocked(req.Tool, req.Args); blocked {
		return fmt.Errorf("%w: %s", ErrNotAllowed, reason)
	}
	if err := ValidateAsyncMode(req.Tool, req.Args); err != nil {
		return err
	}
	return nil
}

func rejectConnectionOverride(tool string, args []string) error {
	shortConnect := hostURIFlags[tool]
	for index, arg := range args {
		if arg == "--" {
			if tool == "virt-admin" && index+1 < len(args) && isConnectCommand(args[index+1]) {
				return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
			}
			break
		}
		longName := ""
		if strings.HasPrefix(arg, "--") {
			longName, _, _ = strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		}
		if longName != "" && strings.HasPrefix("connect", longName) {
			return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
		}
		if shortConnect && arg != "-" && strings.HasPrefix(arg, "-c") {
			return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
		}
		if tool == "virt-admin" && !strings.HasPrefix(arg, "-") && isConnectCommand(arg) {
			return fmt.Errorf("%w: connection URI is managed by the host profile", ErrNotAllowed)
		}
		if strings.ContainsAny(arg, "\r\n\x00") {
			return fmt.Errorf("%w: control character in argument", ErrInvalidArgs)
		}
	}
	return nil
}

// virshCommandOverridesConnection looks for the built-in "connect" command,
// which changes the libvirt connection independently of --connect. Global
// options may precede the command and some of them consume a value.
func virshCommandOverridesConnection(args []string) bool {
	if isConnectCommand(virshCommandName(args)) {
		return true
	}
	// Keep a conservative argv scan as well. GNU getopt permits clustered short
	// flags such as -qc URI, so the command token alone is not sufficient.
	for _, arg := range args {
		if arg == "--" {
			break
		}
		longName := ""
		if strings.HasPrefix(arg, "--") {
			longName, _, _ = strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		}
		if longName != "" && strings.HasPrefix("connect", longName) {
			return true
		}
		if arg != "-" && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(strings.TrimPrefix(arg, "-"), "c") {
			return true
		}
	}
	return false
}

// virshCommandName finds the command token after virsh global options. It is
// shared by the connection-override and blocked-command checks so option
// prefixes or clustered short flags cannot hide the effective command.
func virshCommandName(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 < len(args) {
				return args[index+1]
			}
			return ""
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			return arg
		}
		if strings.HasPrefix(arg, "--") {
			name, _, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if !hasValue && virshLongOptionTakesValue(name) {
				index++
			}
			continue
		}
		// GNU getopt allows short flags to be clustered, and an option that
		// takes a value consumes either the rest of the cluster or the next
		// argv item. Parse that shape before deciding which token is the
		// command so a clustered -c cannot hide a connection override.
		rest := strings.TrimPrefix(arg, "-")
		for offset := 0; offset < len(rest); offset++ {
			option := rest[offset]
			if virshShortOptionTakesValue(option) {
				if offset == len(rest)-1 {
					index++
				}
				break
			}
		}
	}
	return ""
}

func isConnectCommand(command string) bool {
	// vshCommandOpt allows unique command-name prefixes. Reject every prefix
	// conservatively so an abbreviation cannot select connect indirectly.
	return command != "" && strings.HasPrefix("connect", command)
}

func virshLongOptionTakesValue(name string) bool {
	if name == "" {
		return false
	}
	valueOptions := []string{
		"connect", "debug", "escape", "keepalive-count", "keepalive-interval", "log",
	}
	flagOptions := []string{"echo", "help", "quiet", "readonly", "timing", "version"}
	valueMatch := false
	flagMatch := false
	for _, option := range valueOptions {
		if strings.HasPrefix(option, name) {
			valueMatch = true
			break
		}
	}
	for _, option := range flagOptions {
		if strings.HasPrefix(option, name) {
			flagMatch = true
			break
		}
	}
	return valueMatch && !flagMatch
}

func virshShortOptionTakesValue(option byte) bool {
	switch option {
	case 'c', 'd', 'e', 'k', 'K', 'l':
		return true
	}
	return false
}

var toolNames = map[string]struct{}{
	"virsh": {}, "qemu-img": {}, "qemu-io": {}, "qemu-nbd": {},
	"qemu-storage-daemon": {}, "qemu-pr-helper": {}, "qemu-trace-stap": {},
	"qemu-vmsr-helper": {}, "qemu-vnc": {},
}

// UsesHostURI reports whether a tool accepts a libvirt connection URI and
// should therefore be bound to the selected host profile. libguestfs and
// virtinst tools take the same global --connect option as virsh.
func UsesHostURI(tool string) bool {
	_, ok := hostURIFlags[tool]
	return ok
}

// hostURIFlags controls both injection and rejection of user-supplied
// connection options. The boolean records whether the tool's short -c also
// means --connect; virt-install's -c is --cdrom and must remain usable.
var hostURIFlags = map[string]bool{
	"virsh":      true,
	"virt-admin": true, "virt-install": false, "virt-clone": false,
	"virt-convert": false, "virt-xml": true,
	"virt-alignment-scan": true, "virt-cat": true, "virt-copy-in": true,
	"virt-copy-out": true, "virt-customize": true, "virt-df": true,
	"virt-diff": true, "virt-edit": true, "virt-filesystems": true,
	"virt-get-kernel": true, "virt-inspector": true,
	"virt-list-filesystems": true, "virt-list-partitions": true,
	"virt-log": true, "virt-ls": true, "virt-rescue": true,
	"virt-sysprep": true,
	"virt-tail":    true, "virt-tar": true, "virt-tar-in": true,
	"virt-tar-out": true, "virt-win-reg": true, "virt-top": true,
	"guestfish": true, "guestmount": true,
}

func init() {
	for _, name := range config.VirtToolNames {
		toolNames[name] = struct{}{}
	}
}

func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if err := r.Validate(req); err != nil {
		return Result{}, err
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}
	if timeout > r.maxTimeout {
		timeout = r.maxTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := []string{}
	if req.Tool == "virsh" && req.ReadOnly {
		argv = append(argv, "--readonly")
	}
	if UsesHostURI(req.Tool) && req.ConnectURI != "" {
		argv = append(argv, "--connect", req.ConnectURI)
	}
	argv = append(argv, req.Args...)

	if req.SSH != nil {
		script := remote.ExecScript(r.remotePATH, append([]string{req.Tool}, argv...))
		sshArgs, err := req.SSH.CommandArgs(script)
		if err != nil {
			return Result{}, err
		}
		display := append([]string{"ssh", req.SSH.Destination(), "--", req.Tool}, argv...)
		return r.runProcess(runCtx, timeout, r.sshPath, sshArgs, req.Stdin, display)
	}

	display := append([]string{req.Tool}, argv...)
	return r.runProcess(runCtx, timeout, r.paths[req.Tool], argv, req.Stdin, display)
}

func (r *Runner) runProcess(runCtx context.Context, timeout time.Duration, path string, argv []string, stdin string, display []string) (Result, error) {
	stdout := newLimitedBuffer(int64(r.maxOutputBytes))
	stderr := newLimitedBuffer(int64(r.maxOutputBytes))
	// #nosec G204 -- path is resolved from the fixed tool allowlist; argv is never shell-interpreted.
	cmd := exec.Command(path, argv...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = runnerEnv()
	// The panel must not orphan a direct child if it is killed abruptly. A
	// daemonizing grandchild can still escape a process group, which is why
	// ValidateAsyncMode rejects the documented daemon/fork flags above.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	// If a descendant escapes the process group and keeps stdout/stderr open,
	// Wait must still return after the group has been killed.
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitCh:
	case <-runCtx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		err = <-waitCh
	}
	duration := time.Since(start)
	result := Result{
		Stdout:    sanitizeCommandText(stdout.String()),
		Stderr:    sanitizeCommandText(stderr.String()),
		Truncated: stdout.Truncated() || stderr.Truncated(),
		Duration:  duration,
		Command:   display,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("command timed out after %s", timeout)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return result, context.Canceled
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return result, fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}
		return result, err
	}
	return result, nil
}

func runnerEnv() []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true,
		"TZ": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "XDG_RUNTIME_DIR": true,
	}
	// These families carry libvirt/QEMU/libguestfs runtime configuration such as
	// alternate appliance paths, libvirt auth files and QEMU dependency paths.
	// Keep the allowlist narrow so unrelated application secrets never leak into
	// child processes.
	prefixes := []string{"LIBVIRT_", "VIRSH_", "LIBGUESTFS_", "GUESTFS_", "VIRT_", "QEMU_"}
	var env []string
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		allowedKey := allowed[key]
		if !allowedKey {
			for _, prefix := range prefixes {
				if strings.HasPrefix(key, prefix) {
					allowedKey = true
					break
				}
			}
		}
		if allowedKey {
			env = append(env, item)
		}
	}
	// Deterministic locale avoids localized parser output. Reuse an existing
	// key rather than appending duplicate environment entries.
	env = replaceEnv(env, "LC_ALL", "C")
	env = replaceEnv(env, "LANG", "C")
	sort.Strings(env)
	return env
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func sanitizeCommandText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	return strings.ToValidUTF8(value, "�")
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer { return &limitedBuffer{limit: limit} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func CommandLine(tool string, argv []string) string {
	parts := make([]string, 0, len(argv)+1)
	parts = append(parts, tool)
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`;&|<>(){}[]*?!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
