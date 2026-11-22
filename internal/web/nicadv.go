package web

import (
	"fmt"
	"strconv"
	"strings"
)

func nicAdvancedArgs(vm, mac, vlan, vlanMode, mtu, newMAC, link string) ([]string, error) {
	if !validMAC(mac) {
		return nil, fmt.Errorf("请选择网卡")
	}
	parts := []string{}
	if vlan != "" {
		id, err := strconv.Atoi(vlan)
		if err != nil || id < 1 || id > 4094 {
			return nil, fmt.Errorf("VLAN 编号必须在 1 到 4094 之间")
		}
		if vlanMode != "access" && vlanMode != "trunk" {
			return nil, fmt.Errorf("VLAN 模式无效")
		}
		parts = append(parts, vlanMode+"="+vlan)
	}
	if mtu != "" {
		value, err := strconv.Atoi(mtu)
		if err != nil || value < 576 || value > 9000 {
			return nil, fmt.Errorf("MTU 必须在 576 到 9000 之间")
		}
		parts = append(parts, "mtu.size="+mtu)
	}
	if newMAC != "" {
		if !validMAC(newMAC) {
			return nil, fmt.Errorf("新 MAC 地址无效")
		}
		parts = append(parts, "mac="+strings.ToLower(newMAC))
	}
	if link != "" {
		if link != "up" && link != "down" {
			return nil, fmt.Errorf("链路状态无效")
		}
		parts = append(parts, "link="+link)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("请至少选择一项网卡配置")
	}
	return []string{vm, "--edit", "mac=" + strings.ToLower(mac), "--network", strings.Join(parts, ",")}, nil
}
