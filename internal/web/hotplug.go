package web

import (
	"fmt"
	"strings"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/libvirt"
)

func hotplugArgs(action, vm, source, network, target string, devices []libvirt.BlockDevice) ([]string, error) {
	switch action {
	case "disk-attach":
		if err := command.ValidateDevicePathPublic(source); err != nil && !validVolumePath(source) {
			return nil, fmt.Errorf("热添加磁盘只接受存储卷或 /dev 设备")
		}
		if target == "" {
			var err error
			target, err = nextVirtioTarget(devices)
			if err != nil {
				return nil, err
			}
		}
		if err := command.ValidateDiskTargetPublic(target); err != nil || !strings.HasPrefix(target, "vd") {
			return nil, fmt.Errorf("virtio 磁盘目标无效")
		}
		return []string{"attach-disk", vm, source, target, "--targetbus", "virtio", "--live", "--config"}, nil
	case "disk-detach":
		if err := command.ValidateDiskTargetPublic(target); err != nil || !strings.HasPrefix(target, "vd") {
			return nil, fmt.Errorf("只能热卸载 virtio 数据盘")
		}
		return []string{"detach-disk", vm, target, "--live", "--config"}, nil
	case "nic-attach":
		if err := libvirt.ValidateName(network); err != nil {
			return nil, fmt.Errorf("虚拟网络名称无效")
		}
		return []string{"attach-interface", vm, "network", network, "--model", "virtio", "--live", "--config"}, nil
	case "cdrom-change":
		if err := command.ValidateDiskTargetPublic(target); err != nil {
			return nil, fmt.Errorf("光盘目标无效")
		}
		if source == "" {
			return []string{"change-media", vm, target, "--eject", "--live", "--config"}, nil
		}
		if !strings.HasPrefix(source, "/") || strings.Contains(source, "..") || !strings.HasSuffix(strings.ToLower(source), ".iso") {
			return nil, fmt.Errorf("光盘介质必须是 ISO 路径")
		}
		return []string{"change-media", vm, target, "--insert", source, "--live", "--config"}, nil
	default:
		return nil, fmt.Errorf("未知热插拔操作")
	}
}

func nextVirtioTarget(devices []libvirt.BlockDevice) (string, error) {
	used := map[string]bool{}
	for _, device := range devices {
		used[strings.ToLower(device.Target)] = true
	}
	for letter := byte('b'); letter <= byte('z'); letter++ {
		target := "vd" + string(letter)
		if !used[target] {
			return target, nil
		}
	}
	return "", fmt.Errorf("没有可用的 virtio 盘符")
}

func validVolumePath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}
