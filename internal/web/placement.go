package web

import (
	"fmt"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

func placementArgs(action, vm, value string) ([]string, error) {
	if err := libvirt.ValidateName(vm); err != nil || !validCPUSet(value) {
		return nil, fmt.Errorf("放置参数无效")
	}
	switch action {
	case "numa-bind":
		return []string{"numatune", vm, "--nodeset", value, "--config"}, nil
	case "cpu-pin":
		return []string{"vcpupin", vm, "0", value, "--config"}, nil
	default:
		return nil, fmt.Errorf("未知放置操作")
	}
}

func validCPUSet(value string) bool {
	if value == "" || len(value) > 64 || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == ',' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
