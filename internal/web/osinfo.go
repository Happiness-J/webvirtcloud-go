package web

import "fmt"

type osProfile struct {
	ID       string
	Label    string
	Variant  string
	DiskBus  string
	NIC      string
	Firmware string
	VCPUs    int
	MemoryGB int
	DiskGB   int
}

func osProfiles() []osProfile {
	return []osProfile{
		{ID: "generic", Label: "通用", Variant: "generic", DiskBus: "virtio", NIC: "virtio", Firmware: "bios", VCPUs: 2, MemoryGB: 4, DiskGB: 40},
		{ID: "linux", Label: "Linux", Variant: "linux2022", DiskBus: "virtio", NIC: "virtio", Firmware: "uefi", VCPUs: 2, MemoryGB: 4, DiskGB: 40},
		{ID: "windows", Label: "Windows", Variant: "win2k22", DiskBus: "sata", NIC: "e1000e", Firmware: "uefi", VCPUs: 2, MemoryGB: 4, DiskGB: 64},
		{ID: "bsd", Label: "BSD", Variant: "freebsd13.0", DiskBus: "virtio", NIC: "virtio", Firmware: "bios", VCPUs: 2, MemoryGB: 2, DiskGB: 32},
	}
}

type vmSize struct {
	ID       string
	Label    string
	VCPUs    int
	MemoryGB int
	DiskGB   int
}

func vmSizes() []vmSize {
	return []vmSize{
		{ID: "small", Label: "小", VCPUs: 1, MemoryGB: 2, DiskGB: 20},
		{ID: "medium", Label: "中", VCPUs: 2, MemoryGB: 4, DiskGB: 40},
		{ID: "large", Label: "大", VCPUs: 4, MemoryGB: 8, DiskGB: 80},
	}
}

func vmSizeByID(id string) (vmSize, error) {
	for _, item := range vmSizes() {
		if item.ID == id {
			return item, nil
		}
	}
	return vmSize{}, fmt.Errorf("规格档位无效")
}

func osProfileByID(id string) (osProfile, error) {
	for _, item := range osProfiles() {
		if item.ID == id {
			return item, nil
		}
	}
	return osProfile{}, fmt.Errorf("操作系统配置无效")
}
