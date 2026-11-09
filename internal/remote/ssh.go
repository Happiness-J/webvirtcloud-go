// Package remote contains the hardened OpenSSH transport shared by the
// console tunnel and command workers. Every argv element is POSIX-quoted
// before it is sent to the remote shell; callers must never construct a
// remote command line themselves.
package remote

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Target is the SSH endpoint encoded in a qemu+ssh libvirt URI.
type Target struct {
	Host       string
	User       string
	Port       int
	KeyFile    string
	KnownHosts string
}

// FromURI extracts and validates the SSH portion of a libvirt qemu+ssh URI.
// libvirt.URI validation remains the caller's responsibility before use.
func FromURI(libvirtURI string) (Target, error) {
	u, err := url.Parse(libvirtURI)
	if err != nil {
		return Target{}, fmt.Errorf("解析 SSH 目标失败: %w", err)
	}
	if u.Scheme != "qemu+ssh" {
		return Target{}, fmt.Errorf("SSH 执行目标必须使用 qemu+ssh URI")
	}
	target := Target{Host: u.Hostname(), User: u.User.Username()}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return Target{}, fmt.Errorf("SSH 端口必须在 1 到 65535 之间")
		}
		target.Port = n
	}
	query := u.Query()
	target.KeyFile = query.Get("keyfile")
	target.KnownHosts = query.Get("known_hosts")
	if target.KnownHosts == "" {
		target.KnownHosts = query.Get("knownhosts")
	}
	if err := target.Validate(); err != nil {
		return Target{}, err
	}
	return target, nil
}

// Validate performs defense-in-depth checks independent of libvirt URI
// validation. It rejects values that OpenSSH could interpret as options.
func (t Target) Validate() error {
	if t.Host == "" || len(t.Host) > 253 || strings.ContainsAny(t.Host, "\x00\r\n\t @") {
		return fmt.Errorf("SSH 主机名无效")
	}
	if ip := net.ParseIP(t.Host); ip != nil {
		if ip.IsUnspecified() {
			return fmt.Errorf("SSH 主机地址不能为未指定地址")
		}
	} else if strings.HasPrefix(t.Host, "-") || strings.HasPrefix(t.Host, ".") {
		return fmt.Errorf("SSH 主机名无效")
	}
	if t.User != "" {
		if len(t.User) > 64 || t.User[0] == '-' || t.User[0] == '.' {
			return fmt.Errorf("SSH 用户名无效")
		}
		for _, r := range t.User {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
				continue
			}
			return fmt.Errorf("SSH 用户名包含无效字符")
		}
	}
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("SSH 端口必须在 1 到 65535 之间")
	}
	if err := validateAbsentControl(t.KeyFile, "SSH keyfile"); err != nil {
		return err
	}
	if err := validateAbsentControl(t.KnownHosts, "SSH known_hosts"); err != nil {
		return err
	}
	for name, value := range map[string]string{"SSH keyfile": t.KeyFile, "SSH known_hosts": t.KnownHosts} {
		if value != "" && !filepath.IsAbs(value) {
			return fmt.Errorf("%s 必须是绝对路径", name)
		}
	}
	return nil
}

func validateAbsentControl(value, label string) error {
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s 包含控制字符", label)
	}
	return nil
}

// Destination returns the OpenSSH destination argument.
func (t Target) Destination() string {
	if t.User == "" {
		return t.Host
	}
	return t.User + "@" + t.Host
}

// CommandArgs returns OpenSSH arguments that execute script through /bin/sh on
// the target. The script itself must already contain safely quoted argv.
func (t Target) CommandArgs(script string) ([]string, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	args := t.options()
	args = append(args, "-T", "--", t.Destination(), "/bin/sh", "-c", Quote(script))
	return args, nil
}

// StreamArgs returns OpenSSH arguments for an unencrypted stream forwarded to
// address:port relative to the remote host.
func (t Target) StreamArgs(address, port string) ([]string, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if address == "" || strings.ContainsAny(address, " \t\r\n\x00") {
		return nil, fmt.Errorf("SSH 转发地址无效")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("SSH 转发端口无效")
	}
	args := t.options()
	args = append(args, "-o", "ExitOnForwardFailure=yes", "-T", "-W", net.JoinHostPort(strings.Trim(address, "[]"), port), "--", t.Destination())
	return args, nil
}

func (t Target) options() []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "ConnectTimeout=10",
		"-o", "LogLevel=ERROR",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ClearAllForwardings=yes",
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "PermitLocalCommand=no",
		"-o", "RequestTTY=no",
	}
	if t.Port != 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	if t.KeyFile != "" {
		args = append(args, "-i", t.KeyFile, "-o", "IdentitiesOnly=yes")
	}
	if t.KnownHosts != "" {
		args = append(args, "-o", "UserKnownHostsFile="+t.KnownHosts)
	}
	return args
}

// Quote returns one POSIX shell word. Single quotes preserve every byte
// except NUL (which callers reject before reaching this function).
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// ExecScript builds a POSIX sh wrapper that terminates the remote command when
// the SSH channel disappears. The child keeps stdin explicitly because POSIX
// shells redirect asynchronous commands from /dev/null otherwise.
func ExecScript(path string, argv []string) string {
	parts := []string{
		"child=",
		"pgid=",
		`cleanup() { if [ -n "$pgid" ]; then kill -TERM "-$pgid" 2>/dev/null || true; fi; if [ -n "$child" ]; then kill -TERM "$child" 2>/dev/null || true; fi; sleep 1; if [ -n "$pgid" ]; then kill -KILL "-$pgid" 2>/dev/null || true; fi; if [ -n "$child" ]; then kill -KILL "$child" 2>/dev/null || true; fi; }`,
		`trap 'cleanup' HUP INT TERM`,
		"exec 3<&0",
		"PATH=" + Quote(path) + "; export PATH",
		"LC_ALL=C; export LC_ALL",
		"LANG=C; export LANG",
	}
	if len(argv) == 0 {
		return strings.Join(parts, "; ")
	}
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = Quote(arg)
	}
	command := strings.Join(quoted, " ")
	return strings.Join(parts, "; ") + "; " +
		`if command -v setsid >/dev/null 2>&1; then setsid ` + command + ` <&3 & child=$!; pgid=$child; else ` + command + ` <&3 & child=$!; fi; ` +
		`wait "$child"; status=$?; trap - HUP INT TERM; exit "$status"`
}

// PresenceScript returns a POSIX sh script that prints the names of installed
// tools. tools must come from the fixed application registry, never from an
// HTTP request.
func PresenceScript(path string, tools []string) string {
	quoted := make([]string, len(tools))
	for i, tool := range tools {
		quoted[i] = Quote(tool)
	}
	return "PATH=" + Quote(path) + "; export PATH; for t in " + strings.Join(quoted, " ") + "; do command -v \"$t\" >/dev/null 2>&1 && printf '%s\\n' \"$t\"; done"
}

// Environment is the allowlisted environment inherited by local ssh clients.
func Environment() []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true,
		"TZ": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "XDG_RUNTIME_DIR": true,
	}
	env := make([]string, 0, len(allowed))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok || !allowed[key] {
			continue
		}
		switch key {
		case "LC_ALL", "LANG":
			item = key + "=C"
		}
		env = append(env, item)
	}
	return env
}

// ErrNoSSHClient identifies a deployment missing OpenSSH.
var ErrNoSSHClient = errors.New("OpenSSH 客户端未安装")
