package web

import (
	"encoding/xml"
	"strings"
)

// writeThroughDisk is a VM disk whose driver cache is writethrough. The list
// only names the target and never changes the cache mode.
type writeThroughDisk struct {
	Target string
	Source string
}

func writeThroughDisks(domainXML string) []writeThroughDisk {
	var domain struct {
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"`
				Driver struct {
					Cache string `xml:"cache,attr"`
				} `xml:"driver"`
				Source struct {
					File string `xml:"file,attr"`
					Dev  string `xml:"dev,attr"`
				} `xml:"source"`
				Target struct {
					Dev string `xml:"dev,attr"`
				} `xml:"target"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(domainXML), &domain); err != nil {
		return nil
	}
	var out []writeThroughDisk
	for _, disk := range domain.Devices.Disks {
		if disk.Device != "" && disk.Device != "disk" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(disk.Driver.Cache), "writethrough") || disk.Target.Dev == "" {
			continue
		}
		source := disk.Source.File
		if source == "" {
			source = disk.Source.Dev
		}
		out = append(out, writeThroughDisk{Target: disk.Target.Dev, Source: source})
	}
	return out
}
