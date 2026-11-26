package web

import (
	"encoding/xml"
	"strings"
)

// diskBusMismatch is a data disk whose bus is not the virtio bus recommended
// for the generic, Linux, and BSD profiles. The list never changes a bus.
type diskBusMismatch struct {
	Target string
	Bus    string
	Want   string
}

func diskBusMismatches(domainXML string) []diskBusMismatch {
	var domain struct {
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"`
				Target struct {
					Dev string `xml:"dev,attr"`
					Bus string `xml:"bus,attr"`
				} `xml:"target"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(domainXML), &domain); err != nil {
		return nil
	}
	var out []diskBusMismatch
	for _, disk := range domain.Devices.Disks {
		if disk.Device != "" && disk.Device != "disk" {
			continue
		}
		bus := strings.ToLower(strings.TrimSpace(disk.Target.Bus))
		if disk.Target.Dev == "" || bus == "" || bus == "virtio" {
			continue
		}
		out = append(out, diskBusMismatch{Target: disk.Target.Dev, Bus: bus, Want: "virtio"})
	}
	return out
}
