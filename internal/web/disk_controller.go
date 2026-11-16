package web

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type diskController struct {
	Target string
	Bus    string
	Queues int
}

func diskBusArg(bus string, queues int) (string, error) {
	switch bus {
	case "", "virtio":
		return "virtio", nil
	case "sata", "scsi":
		return bus, nil
	case "nvme":
		if queues < 1 || queues > 16 {
			return "", fmt.Errorf("NVMe 队列数必须在 1 到 16 之间")
		}
		return "nvme,queues=" + strconv.Itoa(queues), nil
	default:
		return "", fmt.Errorf("磁盘控制器无效")
	}
}

func parseDiskControllers(xmlText string) []diskController {
	var parsed struct {
		Devices struct {
			Disks []struct {
				Target struct {
					Dev string `xml:"dev,attr"`
					Bus string `xml:"bus,attr"`
				} `xml:"target"`
				Driver struct {
					Queues int `xml:"queues,attr"`
				} `xml:"driver"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return nil
	}
	out := make([]diskController, 0, len(parsed.Devices.Disks))
	for _, disk := range parsed.Devices.Disks {
		if disk.Target.Dev == "" {
			continue
		}
		out = append(out, diskController{Target: disk.Target.Dev, Bus: disk.Target.Bus, Queues: disk.Driver.Queues})
	}
	return out
}

func diskControllerArgs(vm, target, bus string, queues int) ([]string, error) {
	value, err := diskBusArg(bus, queues)
	if err != nil || strings.TrimSpace(target) == "" {
		return nil, err
	}
	return []string{vm, "--edit", "target=" + target, "--disk", "bus=" + value}, nil
}
