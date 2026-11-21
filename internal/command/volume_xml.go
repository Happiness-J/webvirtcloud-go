package command

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type storageVolume struct {
	XMLName  xml.Name                `xml:"volume"`
	Name     string                  `xml:"name"`
	Capacity storageVolumeSize       `xml:"capacity"`
	Target   storageVolumeTarget     `xml:"target"`
}

type storageVolumeSize struct {
	Unit  string `xml:"unit,attr"`
	Value uint64 `xml:",chardata"`
}

type storageVolumeTarget struct {
	Format      storageVolumeFormat      `xml:"format"`
	Permissions storageVolumePermissions `xml:"permissions"`
}

type storageVolumeFormat struct {
	Type string `xml:"type,attr"`
}

type storageVolumePermissions struct {
	Mode  string `xml:"mode"`
	Owner string `xml:"owner"`
	Group string `xml:"group"`
}

func VolumeXML(name, format string, capacity uint64, mode, owner, group int) (string, error) {
	if err := validateVolumeIdentity(name, format); err != nil {
		return "", err
	}
	if capacity == 0 || mode < 0 || mode > 0o777 || owner < 0 || group < 0 {
		return "", fmt.Errorf("卷权限参数无效")
	}
	value, err := xml.Marshal(storageVolume{
		Name:     name,
		Capacity: storageVolumeSize{Unit: "bytes", Value: capacity},
		Target: storageVolumeTarget{
			Format: storageVolumeFormat{Type: format},
			Permissions: storageVolumePermissions{
				Mode:  fmt.Sprintf("%04o", mode),
				Owner: strconv.Itoa(owner),
				Group: strconv.Itoa(group),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return xml.Header + string(value), nil
}

func ValidateVolumeXML(value, name, format string, capacity uint64, mode, owner, group int) error {
	decoder := xml.NewDecoder(strings.NewReader(value))
	decoder.CharsetReader = nil
	var volume storageVolume
	if err := decoder.Decode(&volume); err != nil {
		return fmt.Errorf("卷定义无效")
	}
	if err := decoder.Decode(new(struct{})); err == nil || err.Error() != "EOF" {
		return fmt.Errorf("卷定义无效")
	}
	if volume.Name != name || volume.Capacity.Unit != "bytes" || volume.Capacity.Value != capacity || volume.Target.Format.Type != format {
		return fmt.Errorf("卷定义不匹配")
	}
	if volume.Target.Permissions.Mode != fmt.Sprintf("%04o", mode) || volume.Target.Permissions.Owner != strconv.Itoa(owner) || volume.Target.Permissions.Group != strconv.Itoa(group) {
		return fmt.Errorf("卷权限不匹配")
	}
	return nil
}

func validateVolumeIdentity(name, format string) error {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("卷名称无效")
	}
	switch format {
	case "qcow2", "raw", "qcow", "vmdk", "qed", "iso":
		return nil
	default:
		return fmt.Errorf("卷格式无效")
	}
}