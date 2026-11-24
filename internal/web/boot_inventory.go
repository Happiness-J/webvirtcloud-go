package web

import (
	"encoding/xml"
	"strings"
)

// bootInventory is the firmware and boot state already present in domain XML.
// It is read-only and never changes firmware or boot order.
type bootInventory struct {
	Firmware   string
	SecureBoot string
	TPM        string
	BootOrder  string
	BootMenu   string
	Loader     string
}

func parseBootInventory(xmlText string) bootInventory {
	var parsed struct {
		OS struct {
			Firmware string `xml:"firmware,attr"`
			Loader   struct {
				Type     string `xml:"type,attr"`
				Secure   string `xml:"secure,attr"`
				ReadOnly string `xml:"readonly,attr"`
				Path     string `xml:",chardata"`
			} `xml:"loader"`
			NVRAM struct {
				Path string `xml:",chardata"`
			} `xml:"nvram"`
			BootMenu struct {
				Enable string `xml:"enable,attr"`
			} `xml:"bootmenu"`
			Boots []struct {
				Dev   string `xml:"dev,attr"`
				Order string `xml:"order,attr"`
			} `xml:"boot"`
		} `xml:"os"`
		Devices struct {
			TPMs []struct {
				Model   string `xml:"model,attr"`
				Backend struct {
					Type    string `xml:"type,attr"`
					Version string `xml:"version,attr"`
				} `xml:"backend"`
			} `xml:"tpm"`
		} `xml:"devices"`
	}
	item := bootInventory{Firmware: "BIOS", SecureBoot: "未启用", TPM: "未配置", BootOrder: "未设置", BootMenu: "关闭"}
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return item
	}
	loader := strings.TrimSpace(parsed.OS.Loader.Path)
	item.Loader = loader
	firmware := strings.ToLower(strings.TrimSpace(parsed.OS.Firmware))
	switch {
	case firmware == "efi" || parsed.OS.Loader.Type == "pflash" || strings.Contains(strings.ToLower(loader), "efi") || strings.TrimSpace(parsed.OS.NVRAM.Path) != "":
		item.Firmware = "UEFI"
	case firmware == "bios" || parsed.OS.Loader.Type == "rom" || firmware == "":
		item.Firmware = "BIOS"
	default:
		item.Firmware = "未识别"
	}
	if strings.EqualFold(parsed.OS.Loader.Secure, "yes") {
		item.SecureBoot = "已启用"
	}
	if len(parsed.Devices.TPMs) > 0 {
		tpm := parsed.Devices.TPMs[0]
		version := strings.TrimSpace(tpm.Backend.Version)
		if version == "" {
			version = "未标明版本"
		}
		model := strings.TrimSpace(tpm.Model)
		if model == "" {
			model = "tpm"
		}
		item.TPM = model + " " + version
	}
	var order []string
	names := map[string]string{"hd": "硬盘", "cdrom": "光驱", "network": "网络", "fd": "软盘"}
	for _, boot := range parsed.OS.Boots {
		dev := strings.TrimSpace(boot.Dev)
		if dev == "" {
			continue
		}
		if name := names[dev]; name != "" {
			order = append(order, name)
		} else {
			order = append(order, dev)
		}
	}
	if len(order) > 0 {
		item.BootOrder = strings.Join(order, "，然后")
	}
	if strings.EqualFold(parsed.OS.BootMenu.Enable, "yes") {
		item.BootMenu = "开启"
	}
	return item
}
