package web

import "fmt"

func firewallArgs(vm, mac, direction, protocol, port string, antispoof bool) ([]string, error) {
	if !validMAC(mac) {
		return nil, fmt.Errorf("请选择要保护的网卡")
	}
	switch direction {
	case "in", "out":
	default:
		return nil, fmt.Errorf("防火墙方向无效")
	}
	switch protocol {
	case "tcp", "udp", "icmp":
	default:
		return nil, fmt.Errorf("防火墙协议无效")
	}
	if protocol != "icmp" && (port == "" || len(port) > 5) {
		return nil, fmt.Errorf("端口无效")
	}
	rule := fmt.Sprintf("clean-traffic,%s,%s,%s", direction, protocol, port)
	if antispoof {
		rule += ",antispoof"
	}
	return []string{vm, "--edit", "mac=" + mac, "--network", "filterref=" + rule}, nil
}
