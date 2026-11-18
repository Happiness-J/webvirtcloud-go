package web

import (
	"fmt"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

func bootOrderArg(order, menu string) (string, error) {
	if strings.TrimSpace(order) == "" {
		order = "hd"
	}
	allowed := map[string]bool{"hd": true, "cdrom": true, "network": true}
	parts := strings.Split(order, ",")
	if len(parts) == 0 || len(parts) > 3 {
		return "", fmt.Errorf("启动顺序无效")
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if !allowed[part] || seen[part] {
			return "", fmt.Errorf("启动顺序只能包含硬盘、光驱和网络，且不能重复")
		}
		seen[part] = true
	}
	menuValue := "off"
	if menu == "on" {
		menuValue = "on"
	} else if menu != "" && menu != "off" {
		return "", fmt.Errorf("启动菜单参数无效")
	}
	return strings.Join(parts, ",") + ",menu=" + menuValue, nil
}

func bootEditArgs(vm, order, menu string) ([]string, error) {
	value, err := bootOrderArg(order, menu)
	if err != nil {
		return nil, err
	}
	return []string{vm, "--edit", "--boot", value}, nil
}

func firmwareBoot(order, menu, firmware, secure string) (string, error) {
	value, err := bootOrderArg(order, menu)
	if err != nil {
		return "", err
	}
	switch firmware {
	case "", "bios":
		if secure == "yes" {
			return "", fmt.Errorf("安全启动需要 UEFI")
		}
	case "uefi":
		if secure == "yes" {
			value += ",uefi,secure=on"
		} else {
			value += ",uefi"
		}
	default:
		return "", fmt.Errorf("固件类型无效")
	}
	return value, nil
}

func firmwareEditArgs(vm, order, menu, firmware, secure string) ([]string, error) {
	value, err := firmwareBoot(order, menu, firmware, secure)
	if err != nil {
		return nil, err
	}
	if err := libvirt.ValidateName(vm); err != nil {
		return nil, err
	}
	return []string{vm, "--edit", "--boot", value}, nil
}
