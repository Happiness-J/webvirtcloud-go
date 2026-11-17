package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// HostDevice is a host block LUN or USB device that can be assigned to one VM.
type HostDevice struct {
	Name        string
	Kind        string
	Description string
	Source      string
	Bus         string
	Device      string
	Vendor      string
	Product     string
	AssignedTo  string
	Assignable  bool
	Reason      string
}

// AssignedDevice is a host device already present in a domain definition.
type AssignedDevice struct {
	VM          string
	Kind        string
	Description string
	Source      string
	XML         string
}

type hostdevXML struct {
	Mode   string `xml:"mode,attr"`
	Type   string `xml:"type,attr"`
	Source struct {
		Vendor *struct {
			ID string `xml:"id,attr"`
		} `xml:"vendor"`
		Product *struct {
			ID string `xml:"id,attr"`
		} `xml:"product"`
		Address *struct {
			Bus    string `xml:"bus,attr"`
			Device string `xml:"device,attr"`
		} `xml:"address"`
		Block string `xml:"block"`
	} `xml:"source"`
}

type domainHostdevXML struct {
	XMLName xml.Name `xml:"domain"`
	Name    string   `xml:"name"`
	Devices struct {
		Hostdevs []hostdevXML `xml:"hostdev"`
		Disks    []struct {
			Type   string `xml:"type,attr"`
			Device string `xml:"device,attr"`
			Source struct {
				Dev string `xml:"dev,attr"`
			} `xml:"source"`
			Target struct {
				Dev string `xml:"dev,attr"`
			} `xml:"target"`
		} `xml:"disk"`
	} `xml:"devices"`
}

// ListHostDevices returns assignable host block and USB devices and marks
// devices already claimed by a virtual machine.
func (s *Service) ListHostDevices(ctx context.Context, hostID string) ([]HostDevice, []AssignedDevice, error) {
	usb, usbErr := s.nodeDevices(ctx, hostID, "usb_device")
	storage, storageErr := s.nodeDevices(ctx, hostID, "storage")
	pci, _ := s.nodeDevices(ctx, hostID, "pci")
	mdev, _ := s.nodeDevices(ctx, hostID, "mdev")
	if usbErr != nil && storageErr != nil {
		return nil, nil, fmt.Errorf("无法读取宿主机设备")
	}
	assigned, assignedErr := s.assignedHostDevices(ctx, hostID)
	claimed := map[string]string{}
	for _, item := range assigned {
		claimed[assignmentKey(item.Kind, item.Source)] = item.VM
	}
	devices := make([]HostDevice, 0, len(usb)+len(storage)+len(pci)+len(mdev))
	for _, item := range append(append(append(usb, storage...), pci...), mdev...) {
		key := assignmentKey(item.Kind, item.Source)
		if owner := claimed[key]; owner != "" {
			item.AssignedTo = owner
			item.Assignable = false
			item.Reason = "已分配给 " + owner
		} else if item.Kind == "block" && strings.HasPrefix(item.Source, "/dev/") && isUnsafeBlock(item.Source) {
			item.Assignable = false
			item.Reason = "系统盘或分区不能直接映射"
		} else {
			item.Assignable = true
		}
		devices = append(devices, item)
	}
	if assignedErr != nil {
		for i := range devices {
			devices[i].Assignable = false
			devices[i].Reason = "无法确认设备占用，已拒绝分配"
		}
	}
	return devices, assigned, nil
}

func (s *Service) nodeDevices(ctx context.Context, hostID, capability string) ([]HostDevice, error) {
	res, err := s.run(ctx, hostID, []string{"nodedev-list", "--cap", capability}, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var devices []HostDevice
	for _, name := range nonEmptyLines(res.Stdout) {
		if !validNodeDeviceName(name) {
			continue
		}
		xmlText, dumpErr := s.nodeDeviceXML(ctx, hostID, name)
		if dumpErr != nil {
			continue
		}
		device, ok := parseNodeDevice(name, capability, xmlText)
		if ok {
			devices = append(devices, device)
		}
	}
	return devices, nil
}

func (s *Service) nodeDeviceXML(ctx context.Context, hostID, name string) (string, error) {
	res, err := s.run(ctx, hostID, []string{"nodedev-dumpxml", name}, 10*time.Second)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func (s *Service) assignedHostDevices(ctx context.Context, hostID string) ([]AssignedDevice, error) {
	domains, err := s.ListDomains(ctx, hostID)
	if err != nil {
		return nil, err
	}
	var assigned []AssignedDevice
	for _, domain := range domains {
		xmlText, xmlErr := s.DomainXML(ctx, hostID, domain.Name)
		if xmlErr != nil {
			return nil, xmlErr
		}
		assigned = append(assigned, ParseAssignedHostDevices(domain.Name, xmlText)...)
	}
	return assigned, nil
}

func validNodeDeviceName(value string) bool {
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' && r != ':' {
			return false
		}
	}
	return true
}

type nodeDeviceXML struct {
	Name string `xml:"name"`
	Caps []struct {
		Type    string `xml:"type,attr"`
		Vendor  idName `xml:"vendor"`
		Product idName `xml:"product"`
		Bus     string `xml:"bus"`
		Device  string `xml:"device"`
		Block   string `xml:"block"`
		UUID    string `xml:"uuid"`
		Address *struct {
			Domain   string `xml:"domain,attr"`
			Bus      string `xml:"bus,attr"`
			Slot     string `xml:"slot,attr"`
			Function string `xml:"function,attr"`
		} `xml:"address"`
	} `xml:"capability"`
}

type idName struct {
	ID       string `xml:"id,attr"`
	NameAttr string `xml:"name,attr"`
	NameText string `xml:",chardata"`
}

func (v idName) label() string {
	if strings.TrimSpace(v.NameAttr) != "" {
		return strings.TrimSpace(v.NameAttr)
	}
	return strings.TrimSpace(v.NameText)
}

func parseNodeDevice(name, capability, xmlText string) (HostDevice, bool) {
	var parsed nodeDeviceXML
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return HostDevice{}, false
	}
	for _, cap := range parsed.Caps {
		switch capability {
		case "usb_device":
			if cap.Type != "usb_device" || !validHexID(cap.Vendor.ID) || !validHexID(cap.Product.ID) {
				continue
			}
			label := cap.Product.label()
			if label == "" {
				label = "USB " + cap.Vendor.ID + ":" + cap.Product.ID
			}
			vendor, product := normalizeHexID(cap.Vendor.ID), normalizeHexID(cap.Product.ID)
			return HostDevice{Name: name, Kind: "usb", Description: label, Source: vendor + ":" + product, Vendor: vendor, Product: product}, true
		case "storage":
			if cap.Type != "storage" || !strings.HasPrefix(cap.Block, "/dev/") || strings.Contains(cap.Block, "..") {
				continue
			}
			return HostDevice{Name: name, Kind: "block", Description: cap.Block, Source: cap.Block}, true
		case "pci":
			if cap.Type != "pci" || cap.Address == nil {
				continue
			}
			source := fmt.Sprintf("%s:%s:%s.%s", cap.Address.Domain, cap.Address.Bus, cap.Address.Slot, cap.Address.Function)
			return HostDevice{Name: name, Kind: "pci", Description: "PCI " + source, Source: source}, true
		case "mdev":
			if cap.Type != "mdev" || cap.UUID == "" {
				continue
			}
			return HostDevice{Name: name, Kind: "mdev", Description: "vGPU " + cap.UUID, Source: cap.UUID}, true
		}
	}
	return HostDevice{}, false
}

func normalizeHexID(value string) string { return strings.TrimPrefix(strings.ToLower(value), "0x") }

func validHexID(value string) bool {
	value = normalizeHexID(value)
	if len(value) != 4 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func isUnsafeBlock(path string) bool {
	base := path[strings.LastIndex(path, "/")+1:]
	if strings.HasPrefix(base, "loop") || strings.HasPrefix(base, "dm-") || strings.HasPrefix(base, "zram") || strings.HasPrefix(base, "nbd") {
		return true
	}
	return strings.HasPrefix(base, "nvme") && strings.Contains(base, "n1") && !strings.Contains(base, "p") || strings.HasPrefix(base, "sd") && len(base) == 3 || strings.HasPrefix(base, "vd") && len(base) == 3 || strings.HasPrefix(base, "hd") && len(base) == 3 || strings.HasPrefix(base, "mmcblk") && !strings.Contains(base, "p")
}

// ParseAssignedHostDevices extracts hostdev and LUN disks from domain XML.
func ParseAssignedHostDevices(vm, xmlText string) []AssignedDevice {
	var parsed domainHostdevXML
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return nil
	}
	var out []AssignedDevice
	for _, item := range parsed.Devices.Hostdevs {
		switch item.Type {
		case "usb":
			if item.Source.Vendor == nil || item.Source.Product == nil {
				continue
			}
			source := normalizeHexID(item.Source.Vendor.ID) + ":" + normalizeHexID(item.Source.Product.ID)
			out = append(out, AssignedDevice{VM: vm, Kind: "usb", Description: "USB " + source, Source: source, XML: compactHostdev(item)})
		case "scsi":
			if item.Source.Block == "" || !strings.HasPrefix(item.Source.Block, "/dev/") {
				continue
			}
			out = append(out, AssignedDevice{VM: vm, Kind: "block", Description: item.Source.Block, Source: item.Source.Block, XML: compactHostdev(item)})
		}
	}
	for _, disk := range parsed.Devices.Disks {
		if disk.Type == "block" && disk.Device == "lun" && strings.HasPrefix(disk.Source.Dev, "/dev/") {
			out = append(out, AssignedDevice{VM: vm, Kind: "block", Description: disk.Source.Dev, Source: disk.Source.Dev, XML: compactDisk(disk.Source.Dev, disk.Target.Dev)})
		}
	}
	return out
}

func assignmentKey(kind, source string) string { return kind + ":" + strings.ToLower(source) }

// HostDeviceXML builds the narrow libvirt device XML accepted for assignment.
// diskTarget is only used for block LUNs and must be an unused guest target.
func HostDeviceXML(device HostDevice, diskTarget string) (string, error) {
	if !device.Assignable {
		return "", fmt.Errorf("设备不可分配")
	}
	switch device.Kind {
	case "usb":
		if !validHexID(device.Vendor) || !validHexID(device.Product) {
			return "", fmt.Errorf("USB 设备标识无效")
		}
		return fmt.Sprintf("<hostdev mode='subsystem' type='usb' managed='yes'><source><vendor id='0x%s'/><product id='0x%s'/></source></hostdev>", normalizeHexID(device.Vendor), normalizeHexID(device.Product)), nil
	case "pci":
		domain, bus, slot, function := pciParts(device.Source)
		return fmt.Sprintf("<hostdev mode='subsystem' type='pci' managed='yes'><source><address domain='0x%s' bus='0x%s' slot='0x%s' function='0x%s'/></source></hostdev>", domain, bus, slot, function), nil
	case "mdev":
		if len(device.Source) < 36 {
			return "", fmt.Errorf("介导设备标识无效")
		}
		return fmt.Sprintf("<hostdev mode='subsystem' type='mdev' model='vfio-pci' managed='yes'><source><address uuid='%s'/></source></hostdev>", xmlEscape(device.Source)), nil
	case "block":
		if !strings.HasPrefix(device.Source, "/dev/") || strings.Contains(device.Source, "..") || isUnsafeBlock(device.Source) {
			return "", fmt.Errorf("块设备路径无效")
		}
		if len(diskTarget) < 3 || len(diskTarget) > 8 || !strings.HasPrefix(diskTarget, "sd") {
			return "", fmt.Errorf("磁盘目标无效")
		}
		return fmt.Sprintf("<disk type='block' device='lun'><driver name='qemu' type='raw' cache='none' io='native'/><source dev='%s'/><target dev='%s' bus='scsi'/></disk>", xmlEscape(device.Source), diskTarget), nil
	default:
		return "", fmt.Errorf("不支持的设备类型")
	}
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "'", "&apos;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}

func compactHostdev(item hostdevXML) string {
	if item.Type == "usb" && item.Source.Vendor != nil && item.Source.Product != nil {
		return fmt.Sprintf("<hostdev mode='subsystem' type='usb' managed='yes'><source><vendor id='0x%s'/><product id='0x%s'/></source></hostdev>", normalizeHexID(item.Source.Vendor.ID), normalizeHexID(item.Source.Product.ID))
	}
	if item.Type == "scsi" && strings.HasPrefix(item.Source.Block, "/dev/") {
		return fmt.Sprintf("<disk type='block' device='lun'><driver name='qemu' type='raw' cache='none' io='native'/><source dev='%s'/><target dev='sdb' bus='scsi'/></disk>", xmlEscape(item.Source.Block))
	}
	return ""
}

func compactDisk(source, target string) string {
	if target == "" {
		target = "sdb"
	}
	return fmt.Sprintf("<disk type='block' device='lun'><driver name='qemu' type='raw' cache='none' io='native'/><source dev='%s'/><target dev='%s' bus='scsi'/></disk>", xmlEscape(source), xmlEscape(target))
}

func pciParts(source string) (string, string, string, string) {
	parts := strings.Split(source, ":")
	if len(parts) != 3 {
		return "0000", "00", "00", "0"
	}
	slot, function, _ := strings.Cut(parts[2], ".")
	return strings.TrimPrefix(parts[0], "0x"), strings.TrimPrefix(parts[1], "0x"), strings.TrimPrefix(slot, "0x"), strings.TrimPrefix(function, "0x")
}
