package web

import "testing"

func TestParseBootInventoryUEFI(t *testing.T) {
	xmlText := `<domain><os firmware="efi"><loader type="pflash" readonly="yes" secure="yes">/usr/share/OVMF/OVMF_CODE.secboot.fd</loader><nvram>/var/lib/libvirt/qemu/nvram/demo_VARS.fd</nvram><bootmenu enable="yes"/><boot dev="hd"/><boot dev="network"/></os><devices><tpm model="tpm-crb"><backend type="emulator" version="2.0"/></tpm></devices></domain>`
	got := parseBootInventory(xmlText)
	if got.Firmware != "UEFI" || got.SecureBoot != "已启用" || got.TPM != "tpm-crb 2.0" || got.BootOrder != "硬盘，然后网络" || got.BootMenu != "开启" {
		t.Fatalf("inventory = %#v", got)
	}
	if got.Loader == "" {
		t.Fatal("loader path missing")
	}
}

func TestParseBootInventoryBIOS(t *testing.T) {
	got := parseBootInventory(`<domain><os><type>hvm</type><boot dev="cdrom"/><boot dev="hd"/></os></domain>`)
	if got.Firmware != "BIOS" || got.SecureBoot != "未启用" || got.TPM != "未配置" || got.BootOrder != "光驱，然后硬盘" || got.BootMenu != "关闭" {
		t.Fatalf("bios inventory = %#v", got)
	}
}

func TestParseBootInventoryRejectsGarbage(t *testing.T) {
	got := parseBootInventory("not xml")
	if got.Firmware != "BIOS" || got.BootOrder != "未设置" {
		t.Fatalf("garbage = %#v", got)
	}
}
