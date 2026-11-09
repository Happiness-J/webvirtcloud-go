package command

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"webvirtcloud-go/internal/remote"
)

// TargetKind identifies where a command will actually execute.
type TargetKind string

const (
	TargetLocal   TargetKind = "local"
	TargetSSH     TargetKind = "ssh"
	TargetLibvirt TargetKind = "libvirt"
)

// Target is derived from a host profile's encrypted libvirt URI. SSH is set
// only for qemu+ssh. ConnectURI is never persisted separately: the worker
// re-resolves it from the host row for every job.
type Target struct {
	Kind       TargetKind
	SSH        *remote.Target
	ConnectURI string
	Label      string
}

// ResolveTarget maps a validated libvirt URI to its safe execution model.
func ResolveTarget(libvirtURI string) (Target, error) {
	if err := ValidateURI(libvirtURI); err != nil {
		return Target{}, err
	}
	u, err := url.Parse(libvirtURI)
	if err != nil {
		return Target{}, fmt.Errorf("解析 libvirt 连接 URI 失败: %w", err)
	}
	switch u.Scheme {
	case "qemu+ssh":
		sshTarget, err := remote.FromURI(libvirtURI)
		if err != nil {
			return Target{}, err
		}
		remoteURI := "qemu:///" + strings.TrimPrefix(u.EscapedPath(), "/")
		if err := ValidateURI(remoteURI); err != nil {
			return Target{}, fmt.Errorf("无法推导目标主机本地 libvirt URI: %w", err)
		}
		return Target{
			Kind:       TargetSSH,
			SSH:        &sshTarget,
			ConnectURI: remoteURI,
			Label:      sshTarget.Destination(),
		}, nil
	case "qemu", "qemu+unix":
		return Target{Kind: TargetLocal, ConnectURI: libvirtURI, Label: "面板本机"}, nil
	case "qemu+tcp", "qemu+tls":
		label := u.Scheme + "://" + u.Host
		return Target{Kind: TargetLibvirt, ConnectURI: libvirtURI, Label: label}, nil
	default:
		return Target{}, fmt.Errorf("不支持的 libvirt 连接类型: %s", u.Scheme)
	}
}

// Supports reports whether a tool can be run against this target. Tools that
// do not accept --connect need an SSH endpoint because they cannot otherwise
// address another physical machine.
func (t Target) Supports(tool string) error {
	if t.Kind == TargetLibvirt && !UsesHostURI(tool) {
		return fmt.Errorf("%s 不接受 libvirt 连接 URI；该主机使用 qemu+tcp/qemu+tls，无法安全地把独立 QEMU 工具发到目标物理机，请改用 qemu+ssh:// 主机配置", tool)
	}
	return nil
}

// Request binds a tool invocation to this target. ConnectURI is populated only
// for tools that understand it; the SSH transport remains separate.
func (t Target) Request(tool string, args []string, stdin string, readOnly bool, timeout time.Duration) Request {
	connectURI := ""
	if UsesHostURI(tool) {
		connectURI = t.ConnectURI
	}
	return Request{
		Tool: tool, Args: args, Stdin: stdin, ConnectURI: connectURI,
		ReadOnly: readOnly, Timeout: timeout, SSH: t.SSH,
	}
}

// LocalToolAvailable reports whether this target needs a locally installed
// executable. SSH targets use the fixed remote PATH instead.
func (t Target) LocalToolAvailable() bool { return t.SSH == nil }
