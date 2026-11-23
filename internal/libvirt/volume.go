package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type VolumeMetadata struct {
	Name     string
	Capacity uint64
	Format   string
}

func (s *Service) VolumeMetadata(ctx context.Context, hostID, pool, name string) (VolumeMetadata, error) {
	if err := ValidateName(pool); err != nil {
		return VolumeMetadata{}, fmt.Errorf("存储池名称无效")
	}
	if err := ValidateName(name); err != nil {
		return VolumeMetadata{}, fmt.Errorf("存储卷名称无效")
	}
	res, err := s.run(ctx, hostID, []string{"vol-dumpxml", "--pool", pool, name}, 10*time.Second)
	if err != nil {
		return VolumeMetadata{}, fmt.Errorf("读取存储卷元数据失败: %s", strings.TrimSpace(res.Output()))
	}
	return ParseVolumeXML(res.Stdout)
}

func ParseVolumeXML(value string) (VolumeMetadata, error) {
	var parsed struct {
		Name     string `xml:"name"`
		Capacity struct {
			Unit  string `xml:"unit,attr"`
			Value string `xml:",chardata"`
		} `xml:"capacity"`
		Target struct {
			Format struct {
				Type string `xml:"type,attr"`
			} `xml:"format"`
		} `xml:"target"`
	}
	if err := xml.Unmarshal([]byte(value), &parsed); err != nil {
		return VolumeMetadata{}, fmt.Errorf("存储卷 XML 无效")
	}
	if parsed.Name == "" {
		return VolumeMetadata{}, fmt.Errorf("存储卷 XML 缺少名称")
	}
	capacity, err := parseVolumeXMLCapacity(parsed.Capacity.Unit, parsed.Capacity.Value)
	if err != nil {
		return VolumeMetadata{}, err
	}
	format := strings.ToLower(strings.TrimSpace(parsed.Target.Format.Type))
	if format == "" {
		return VolumeMetadata{}, fmt.Errorf("存储卷 XML 缺少格式")
	}
	return VolumeMetadata{Name: parsed.Name, Capacity: capacity, Format: format}, nil
}

func parseVolumeXMLCapacity(unit, value string) (uint64, error) {
	number, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("存储卷容量无效")
	}
	if number == 0 {
		return 0, fmt.Errorf("存储卷容量无效")
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "bytes", "b":
		return number, nil
	case "kib", "ki":
		return checkedVolumeCapacity(number, 1024)
	case "mib", "mi":
		return checkedVolumeCapacity(number, 1024*1024)
	case "gib", "gi":
		return checkedVolumeCapacity(number, 1024*1024*1024)
	case "tib", "ti":
		return checkedVolumeCapacity(number, 1024*1024*1024*1024)
	default:
		return 0, fmt.Errorf("不支持的存储卷容量单位")
	}
}

func checkedVolumeCapacity(number, multiplier uint64) (uint64, error) {
	if number > ^uint64(0)/multiplier {
		return 0, fmt.Errorf("存储卷容量溢出")
	}
	return number * multiplier, nil
}
