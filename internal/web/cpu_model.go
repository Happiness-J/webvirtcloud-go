package web

import (
	"fmt"
	"strings"
)

func cpuModelArg(mode, model string, nested bool) (string, error) {
	switch mode {
	case "host-model", "host-passthrough":
	case "custom":
		if !validCPUModel(model) {
			return "", fmt.Errorf("自定义 CPU 模型无效")
		}
		mode = model
	default:
		return "", fmt.Errorf("CPU 模式无效")
	}
	if nested {
		return mode + ",+vmx", nil
	}
	return mode, nil
}

func validCPUModel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func appendCPU(args []string, cpu string) []string {
	if strings.TrimSpace(cpu) == "" {
		return args
	}
	return append(args, "--cpu", cpu)
}
