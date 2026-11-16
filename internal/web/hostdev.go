package web

import (
	"fmt"
	"net/http"

	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/libvirt"
)

func (s *Server) hostdevAction(r *http.Request, hostID, vmName, action, deviceName string) ([]string, string, error) {
	devices, assigned, err := s.libvirt.ListHostDevices(r.Context(), hostID)
	if err != nil {
		return nil, "", fmt.Errorf("无法读取宿主机设备")
	}
	scope := "--config"
	detail, detailErr := s.libvirt.Detail(r.Context(), hostID, vmName, false)
	if detailErr == nil && (detail.Domain.State == "running" || detail.Domain.State == "paused") {
		scope = "--live"
	}
	switch action {
	case "hostdev-attach":
		device, ok := findHostDevice(devices, deviceName)
		if !ok || !device.Assignable {
			return nil, "", fmt.Errorf("设备不存在、已被占用或不能分配")
		}
		target := ""
		if device.Kind == "block" {
			target, err = nextDiskTarget(detail.BlockList)
			if err != nil {
				return nil, "", err
			}
		}
		xmlText, err := libvirt.HostDeviceXML(device, target)
		if err != nil || command.ValidateHostdevXML(xmlText) != nil {
			return nil, "", fmt.Errorf("无法生成受控的设备描述")
		}
		return []string{"attach-device", vmName, scope}, xmlText, nil
	case "hostdev-detach":
		item, ok := findAssignedDevice(assigned, vmName, deviceName)
		if !ok || command.ValidateHostdevXML(item.XML) != nil {
			return nil, "", fmt.Errorf("这台虚拟机没有可安全移除的主机设备")
		}
		return []string{"detach-device", vmName, scope}, item.XML, nil
	default:
		return nil, "", fmt.Errorf("未知操作")
	}
}

func findHostDevice(devices []libvirt.HostDevice, name string) (libvirt.HostDevice, bool) {
	for _, device := range devices {
		if device.Name == name {
			return device, true
		}
	}
	return libvirt.HostDevice{}, false
}

func findAssignedDevice(devices []libvirt.AssignedDevice, vm, source string) (libvirt.AssignedDevice, bool) {
	for _, device := range devices {
		if device.VM == vm && (device.Source == source || device.Description == source) {
			return device, true
		}
	}
	return libvirt.AssignedDevice{}, false
}
