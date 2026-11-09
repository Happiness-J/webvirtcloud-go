package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/remote"
)

type DialOptions struct {
	// SSHPath is the OpenSSH client executable configured through
	// WVC_SSH_BIN. An empty value resolves "ssh" from PATH.
	SSHPath string
	// AllowInsecureVNC permits direct, unencrypted VNC connections for
	// qemu+tcp and qemu+tls management URIs. It is off by default because the
	// current proxy speaks plain VNC after connecting to the host.
	AllowInsecureVNC bool
	// LoopbackAddress replaces a VNC listener bound to loopback/unspecified when
	// the panel runs in a container and the libvirt host is the Docker host.
	// It never affects qemu+ssh or non-loopback remote addresses.
	LoopbackAddress string
}

type Backend struct {
	conn      net.Conn
	cmd       *exec.Cmd
	stderr    *lockedBuffer
	done      chan struct{}
	waitMu    sync.Mutex
	waitErr   error
	closeOnce sync.Once
	closeErr  error
}

func (b *Backend) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		if b.conn != nil {
			if err := b.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, os.ErrClosed) {
				b.closeErr = err
			}
		}
		if b.done != nil {
			select {
			case <-b.done:
				// Wait has already reaped the process. Signalling its old PID or
				// process group could target an unrelated process after PID reuse.
				return
			default:
			}
		}
		b.killProcessGroup()
		if b.done != nil {
			select {
			case <-b.done:
			case <-time.After(3 * time.Second):
				// The process group was already signalled. Give the reaper a
				// final chance, but never block the request forever if the
				// kernel or a broken SSH wrapper refuses to reap it.
				select {
				case <-b.done:
					return
				default:
				}
				b.killProcessGroup()
				select {
				case <-b.done:
				case <-time.After(time.Second):
					if b.closeErr == nil {
						b.closeErr = fmt.Errorf("timed out waiting for console backend process to exit")
					}
				}
			}
		}
	})
	return b.closeErr
}

func (b *Backend) killProcessGroup() {
	if b.cmd == nil || b.cmd.Process == nil || b.cmd.Process.Pid <= 0 {
		return
	}
	_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
}

func (b *Backend) Error() string {
	if b.stderr != nil && strings.TrimSpace(b.stderr.String()) != "" {
		return strings.TrimSpace(b.stderr.String())
	}
	b.waitMu.Lock()
	defer b.waitMu.Unlock()
	if b.waitErr != nil {
		return b.waitErr.Error()
	}
	return "控制台连接已关闭"
}

// Dial connects the panel to the VNC TCP endpoint. qemu+ssh hosts are reached
// through OpenSSH's -W stream forwarding, with BatchMode and strict host key
// checking enabled. No shell is involved.
func Dial(ctx context.Context, libvirtURI string, con libvirt.Console, opts DialOptions) (*Backend, error) {
	if err := command.ValidateURI(libvirtURI); err != nil {
		return nil, fmt.Errorf("libvirt URI 无效: %w", err)
	}
	u, err := url.Parse(libvirtURI)
	if err != nil {
		return nil, fmt.Errorf("解析 libvirt URI 失败: %w", err)
	}
	port := strconv.Itoa(con.Port)
	if port == "0" || con.Port < 1 || con.Port > 65535 {
		return nil, fmt.Errorf("VNC 端口无效")
	}
	switch u.Scheme {
	case "qemu+ssh":
		sshBin, err := resolveSSHPath(opts.SSHPath)
		if err != nil {
			return nil, err
		}
		return dialSSH(ctx, u, con.Address, port, sshBin)
	case "qemu+tcp", "qemu+tls":
		if !opts.AllowInsecureVNC {
			return nil, fmt.Errorf("远程 VNC 未加密；请使用 qemu+ssh 隧道，或显式设置 WVC_ALLOW_INSECURE_VNC=true")
		}
		host := remoteConsoleAddress(u.Hostname(), con.Address)
		return dialPrivateTCP(ctx, host, port)
	case "qemu", "qemu+unix":
		address := localConsoleAddress(con.Address, opts.LoopbackAddress)
		return dialPrivateTCP(ctx, address, port)
	default:
		return nil, fmt.Errorf("不支持的 libvirt URI 类型: %s", u.Scheme)
	}
}

// remoteConsoleAddress prefers the address advertised by libvirt when it is
// more specific than the management URI. A loopback or wildcard console
// address is only reachable on the managed host itself, so direct TCP/TLS
// connections fall back to the management host instead.
func remoteConsoleAddress(managementHost, address string) string {
	managementHost = strings.TrimSpace(managementHost)
	address = strings.TrimSpace(address)
	if isLoopbackOrUnspecified(address) {
		return managementHost
	}
	return address
}

func localConsoleAddress(address, rewrite string) string {
	address = strings.TrimSpace(address)
	rewrite = strings.TrimSpace(rewrite)
	if isLoopbackOrUnspecified(address) && rewrite != "" {
		return rewrite
	}
	switch address {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	}
	return address
}

func isLoopbackOrUnspecified(address string) bool {
	if address == "" || strings.EqualFold(address, "localhost") {
		return true
	}
	ip := net.ParseIP(address)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func dialTCP(ctx context.Context, address string) (*Backend, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("连接 VNC %s 失败: %w", address, err)
	}
	return &Backend{conn: conn}, nil
}

func resolveSSHPath(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "ssh"
	}
	sshBin, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("qemu+ssh 控制台需要安装 OpenSSH 客户端: %w", err)
	}
	return sshBin, nil
}

func dialSSH(ctx context.Context, u *url.URL, address, port, sshBin string) (*Backend, error) {
	sshTarget, err := remote.FromURI(u.String())
	if err != nil {
		return nil, err
	}
	targetHost := sshTarget.Host
	if targetHost == "" {
		return nil, fmt.Errorf("qemu+ssh URI 缺少主机名")
	}
	forwardAddress, err := sshForwardAddress(address)
	if err != nil {
		return nil, err
	}
	args, err := sshDialArgs(u, targetHost, forwardAddress, port)
	if err != nil {
		return nil, err
	}

	// #nosec G204 -- sshBin is a configured executable and every argument is passed as a distinct argv item.
	cmd := exec.Command(sshBin, args...)
	cmd.Env = remote.Environment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &lockedBuffer{limit: 64 * 1024}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 SSH 控制台隧道失败: %w", err)
	}
	rw := &streamConn{Reader: stdout, Writer: stdin, local: streamAddr("ssh"), remote: streamAddr(net.JoinHostPort(targetHost, port))}
	b := &Backend{conn: rw, cmd: cmd, stderr: stderr, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		b.waitMu.Lock()
		b.waitErr = err
		b.waitMu.Unlock()
		close(b.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = b.Close()
		case <-b.done:
		}
	}()
	return b, nil
}

func sshDialArgs(u *url.URL, targetHost, forwardAddress, port string) ([]string, error) {
	sshTarget, err := remote.FromURI(u.String())
	if err != nil {
		return nil, err
	}
	if sshTarget.Host != targetHost {
		return nil, fmt.Errorf("SSH 目标主机不一致")
	}
	return sshTarget.StreamArgs(forwardAddress, port)
}

func sshForwardAddress(host string) (string, error) {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "localhost":
		return "127.0.0.1", nil
	case "::":
		return "::1", nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname would be resolved in the remote SSH server's network
		// namespace, which this process cannot validate for SSRF safety.
		return "", fmt.Errorf("qemu+ssh 控制台的 VNC 地址必须是私有 IP 或 localhost")
	}
	if err := checkPrivateIP(ip); err != nil {
		return "", fmt.Errorf("VNC 目标地址不在允许的私有地址范围内: %w", err)
	}
	return ip.String(), nil
}

func dialPrivateTCP(ctx context.Context, host, port string) (*Backend, error) {
	addresses, err := resolvePrivateTargets(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range addresses {
		backend, err := dialTCP(ctx, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return backend, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("VNC 目标没有可用地址")
	}
	return nil, lastErr
}

func resolvePrivateTargets(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("VNC 目标地址为空")
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkPrivateIP(ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	if strings.EqualFold(host, "localhost") {
		return []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("无法解析 VNC 目标地址 %q", host)
	}
	out := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if err := checkPrivateIP(address.IP); err != nil {
			return nil, fmt.Errorf("VNC 目标地址 %q 不在允许的私有地址范围内: %w", host, err)
		}
		out = append(out, address.IP)
	}
	return out, nil
}

func checkPrivateIP(ip net.IP) error {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("地址不可用于控制台连接")
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return nil
	}
	return fmt.Errorf("仅允许回环或私有网络地址")
}

type lockedBuffer struct {
	mu        sync.Mutex
	b         strings.Builder
	limit     int
	truncated bool
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		return b.b.Write(p)
	}
	remaining := b.limit - b.b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.b.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type streamConn struct {
	io.Reader
	io.Writer
	local  net.Addr
	remote net.Addr
	once   sync.Once
	err    error
}

func (c *streamConn) Close() error {
	c.once.Do(func() {
		if closer, ok := c.Writer.(io.Closer); ok {
			c.err = closer.Close()
		}
		if closer, ok := c.Reader.(io.Closer); ok {
			if e := closer.Close(); c.err == nil {
				c.err = e
			}
		}
	})
	return c.err
}
func (c *streamConn) LocalAddr() net.Addr  { return c.local }
func (c *streamConn) RemoteAddr() net.Addr { return c.remote }

// os.Pipe-backed SSH streams have no deadline support. Cancellation is
// handled by closing the pipes/process in Backend.Close, which unblocks both
// readers and writers. The proxy additionally uses WebSocket ping/pong to
// detect dead browsers.
func (c *streamConn) SetDeadline(time.Time) error      { return nil }
func (c *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *streamConn) SetWriteDeadline(time.Time) error { return nil }

type streamAddr string

func (a streamAddr) Network() string { return "ssh" }
func (a streamAddr) String() string  { return string(a) }
