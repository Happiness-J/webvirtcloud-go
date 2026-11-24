package web

import (
	"encoding/xml"
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

type hostTopology struct {
	Nodes string
	CPUs  string
}

func parseHostTopology(capabilitiesXML string) hostTopology {
	var parsed struct {
		Cells []struct {
			ID   string `xml:"id,attr"`
			CPUs []struct {
				ID string `xml:"id,attr"`
			} `xml:"cpus>cpu"`
		} `xml:"host>topology>cells>cell"`
	}
	if err := xml.Unmarshal([]byte(capabilitiesXML), &parsed); err != nil || len(parsed.Cells) == 0 {
		return hostTopology{Nodes: "未读取到拓扑", CPUs: "未读取到拓扑"}
	}
	nodes := make([]string, 0, len(parsed.Cells))
	cpuCount := 0
	for _, cell := range parsed.Cells {
		nodes = append(nodes, cell.ID)
		cpuCount += len(cell.CPUs)
	}
	cpus := "0"
	if cpuCount > 1 {
		cpus = fmt.Sprintf("0-%d", cpuCount-1)
	}
	return hostTopology{Nodes: strings.Join(nodes, ", "), CPUs: cpus}
}
