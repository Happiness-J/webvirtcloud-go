package web

import (
	"fmt"
	"net"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// consoleSecurityWarnings highlights host-side VNC settings that expose a
// console beyond the panel's authenticated proxy. The panel can still manage
// these VMs, but operators must deliberately correct the libvirt XML.
func consoleSecurityWarnings(con libvirt.Console) []string {
	address := strings.TrimSpace(con.Address)
	password := strings.TrimSpace(con.Password)
	var warnings []string

	switch {
	case address == "" || address == "0.0.0.0" || address == "::":
		warnings = append(warnings,
			"VNC 正在监听所有网络接口。请将 libvirt graphics listen 改为 127.0.0.1，并只通过面板的受控代理访问。")
	case isConsoleLoopback(address):
		// A loopback listener is only reachable from the KVM host itself.
	default:
		warnings = append(warnings,
			fmt.Sprintf("VNC 正在监听非回环地址 %s，局域网客户端可能绕过面板直接连接。建议改为 127.0.0.1。", address))
	}

	if password == "" {
		warnings = append(warnings,
			"该 VNC 控制台未设置密码。如果监听地址不是回环地址，同网段客户端可直接接管控制台。")
	}
	return warnings
}

func isConsoleLoopback(address string) bool {
	if strings.EqualFold(address, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(address, "[]"))
	return ip != nil && ip.IsLoopback()
}
