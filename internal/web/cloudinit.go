package web

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"webvirtcloud-go/internal/id"
)

type cloudInitSpec struct {
	Hostname string
	Username string
	SSHKey   string
	Address  string
	Gateway  string
	DNS      string
}

func (s cloudInitSpec) enabled() bool {
	return s.Hostname != "" || s.Username != "" || s.SSHKey != "" || s.Address != "" || s.Gateway != "" || s.DNS != ""
}

func (s cloudInitSpec) validate() error {
	if !s.enabled() {
		return nil
	}
	if err := validateCloudHostname(s.Hostname); err != nil {
		return err
	}
	if err := validateCloudUser(s.Username); err != nil {
		return err
	}
	if s.SSHKey != "" && !validSSHPublicKey(s.SSHKey) {
		return fmt.Errorf("SSH 公钥格式无效")
	}
	if s.Address != "" {
		if _, _, err := net.ParseCIDR(s.Address); err != nil {
			return fmt.Errorf("客户机地址必须使用 CIDR，例如 192.0.2.10/24")
		}
	}
	if s.Gateway != "" && net.ParseIP(s.Gateway) == nil {
		return fmt.Errorf("网关地址无效")
	}
	if (s.Address == "") != (s.Gateway == "") {
		return fmt.Errorf("静态网络需要同时填写地址和网关")
	}
	for _, server := range strings.Fields(s.DNS) {
		if net.ParseIP(server) == nil {
			return fmt.Errorf("DNS 地址无效")
		}
	}
	return nil
}

func validateCloudHostname(value string) error {
	if value == "" || len(value) > 253 {
		return fmt.Errorf("主机名长度无效")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("主机名无效")
		}
		for _, r := range label {
			if !(r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
				return fmt.Errorf("主机名只能包含字母、数字、点和连字符")
			}
		}
	}
	return nil
}

func validateCloudUser(value string) error {
	if value == "" || len(value) > 32 || strings.HasPrefix(value, "-") {
		return fmt.Errorf("用户名无效")
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return fmt.Errorf("用户名只能包含字母、数字、下划线和连字符")
		}
	}
	return nil
}

func validSSHPublicKey(value string) bool {
	if strings.ContainsAny(value, "\r\n\x00") || len(value) > 4096 {
		return false
	}
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 3 {
		return false
	}
	switch fields[0] {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com":
	default:
		return false
	}
	if len(fields[1]) < 16 || strings.ContainsAny(fields[1], "/+=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") && strings.Trim(fields[1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/=") != "" {
		return false
	}
	return len(fields) < 3 || !strings.ContainsAny(fields[2], "\r\n\x00")
}

func writeCloudInitSeed(root string, spec cloudInitSpec) (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	if !spec.enabled() {
		return "", nil
	}
	dir := filepath.Join(root, "cloud-init")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("无法创建 cloud-init 目录")
	}
	path := filepath.Join(dir, id.New("cidata")+".iso")
	content := renderCloudInitSeed(spec)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("无法写入 cloud-init 配置")
	}
	return path, nil
}

func renderCloudInitSeed(spec cloudInitSpec) string {
	var user strings.Builder
	user.WriteString("#cloud-config\nhostname: ")
	user.WriteString(yamlScalar(spec.Hostname))
	user.WriteString("\nmanage_etc_hosts: true\nusers:\n  - name: ")
	user.WriteString(yamlScalar(spec.Username))
	user.WriteString("\n    sudo: ALL=(ALL) NOPASSWD:ALL\n    groups: sudo\n    shell: /bin/bash\n    lock_passwd: true\n")
	if spec.SSHKey != "" {
		user.WriteString("    ssh_authorized_keys:\n      - ")
		user.WriteString(yamlScalar(spec.SSHKey))
		user.WriteByte('\n')
	}
	user.WriteString("ssh_pwauth: false\n")
	network := "version: 2\n"
	if spec.Address != "" {
		network += "ethernets:\n  id0:\n    match:\n      driver: virtio\n    dhcp4: false\n    addresses:\n      - " + yamlScalar(spec.Address) + "\n    routes:\n      - to: default\n        via: " + yamlScalar(spec.Gateway) + "\n"
		if spec.DNS != "" {
			network += "    nameservers:\n      addresses: [" + yamlList(strings.Fields(spec.DNS)) + "]\n"
		}
	}
	return user.String() + "\n--- network-config ---\n" + network
}

func yamlScalar(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func yamlList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = yamlScalar(value)
	}
	return strings.Join(quoted, ", ")
}
