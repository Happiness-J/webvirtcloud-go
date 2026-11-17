package web

import "fmt"

type osProfile struct {
	ID       string
	Label    string
	Variant  string
	DiskBus  string
	NIC      string
	Firmware string
}

func osProfiles() []osProfile {
	return []osProfile{
		{ID: "generic", Label: "通用", Variant: "generic", DiskBus: "virtio", NIC: "virtio", Firmware: "bios"},
		{ID: "linux", Label: "Linux", Variant: "linux2022", DiskBus: "virtio", NIC: "virtio", Firmware: "uefi"},
		{ID: "windows", Label: "Windows", Variant: "win2k22", DiskBus: "sata", NIC: "e1000e", Firmware: "uefi"},
		{ID: "bsd", Label: "BSD", Variant: "freebsd13.0", DiskBus: "virtio", NIC: "virtio", Firmware: "bios"},
	}
}

func osProfileByID(id string) (osProfile, error) {
	for _, item := range osProfiles() {
		if item.ID == id {
			return item, nil
		}
	}
	return osProfile{}, fmt.Errorf("操作系统配置无效")
}
