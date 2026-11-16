package web

import (
	"fmt"
	"net"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// consoleSecurityWarnings highlights graphics listeners that can be reached
// outside the panel. VNC and SPICE follow the same loopback and password rules.
func consoleSecurityWarnings(con libvirt.Console) []string {
	address := strings.TrimSpace(con.Address)
	password := strings.TrimSpace(con.Password)
	protocol := strings.ToUpper(con.Type)
	if protocol == "" {
		protocol = "图形控制台"
	}
	var warnings []string
	switch {
	case address == "" || address == "0.0.0.0" || address == "::":
		warnings = append(warnings, protocol+" 正在监听所有网络接口。请改为 127.0.0.1，并只通过受控入口访问。")
	case isConsoleLoopback(address):
	default:
		warnings = append(warnings, fmt.Sprintf("%s 正在监听非回环地址 %s，局域网客户端可能绕过面板直接连接。建议改为 127.0.0.1。", protocol, address))
	}
	if password == "" {
		warnings = append(warnings, protocol+" 未设置密码。如果监听地址不是回环地址，同网段客户端可直接连接。")
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
