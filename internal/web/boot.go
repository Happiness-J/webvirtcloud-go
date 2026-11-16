package web

import (
	"fmt"
	"strings"
)

func bootOrderArg(order, menu string) (string, error) {
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
