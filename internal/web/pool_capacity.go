package web

import (
	"fmt"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

// poolCapacityHint compares a pool with the sum of volume capacities.
// It is read-only and never changes allocation or blocks creation.
type poolCapacityHint struct {
	Known       bool
	Over        bool
	Capacity    string
	Committed   string
	Remaining   string
	UsedPercent int
	Label       string
	Detail      string
}

func poolCapacityHints(pools []libvirt.Pool, volumes []libvirt.Volume) map[string]poolCapacityHint {
	out := map[string]poolCapacityHint{}
	for _, pool := range pools {
		out[pool.Name] = summarizePoolCapacity(pool, nil)
	}
	if len(volumes) == 0 {
		return out
	}
	poolName := volumes[0].Pool
	pool, ok := findPool(pools, poolName)
	if !ok {
		return out
	}
	out[poolName] = summarizePoolCapacity(pool, volumes)
	return out
}

func summarizePoolCapacity(pool libvirt.Pool, volumes []libvirt.Volume) poolCapacityHint {
	capacity, capOK := parseReadableBytes(pool.Capacity)
	hint := poolCapacityHint{Capacity: fallbackSize(pool.Capacity), Label: "容量未知"}
	if !capOK || capacity == 0 {
		hint.Detail = "存储池容量还没有读到，暂时不能判断是否超配。"
		return hint
	}
	var committed uint64
	known := 0
	for _, volume := range volumes {
		size, ok := parseReadableBytes(volume.Capacity)
		if !ok {
			continue
		}
		known++
		if committed > ^uint64(0)-size {
			committed = ^uint64(0)
			break
		}
		committed += size
	}
	hint.Known = known > 0
	hint.Committed = humanByteLabel(committed)
	if committed > capacity {
		hint.Over = true
		hint.Remaining = "已超出 " + humanByteLabel(committed-capacity)
		hint.UsedPercent = 100
		hint.Label = "已超配"
		hint.Detail = fmt.Sprintf("卷的承诺容量合计 %s，超过存储池容量 %s。精简置备现在还能放，但一起写满时空间不够。", hint.Committed, hint.Capacity)
		return hint
	}
	free := capacity - committed
	hint.Remaining = "还剩 " + humanByteLabel(free)
	hint.UsedPercent = int(committed * 100 / capacity)
	hint.Label = "未超配"
	if !hint.Known {
		hint.Detail = "打开这个存储池后，会把卷的承诺容量加总，并和池容量比较。"
		hint.Committed = "未统计"
		hint.Remaining = "可用 " + fallbackSize(pool.Available)
		return hint
	}
	hint.Detail = fmt.Sprintf("卷的承诺容量合计 %s，存储池容量 %s。%s。", hint.Committed, hint.Capacity, hint.Remaining)
	return hint
}

func findPool(pools []libvirt.Pool, name string) (libvirt.Pool, bool) {
	for _, pool := range pools {
		if pool.Name == name {
			return pool, true
		}
	}
	return libvirt.Pool{}, false
}

func parseReadableBytes(value string) (uint64, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 {
		return 0, false
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || number < 0 {
		return 0, false
	}
	scale := map[string]float64{"B": 1, "KB": 1024, "MB": 1024 * 1024, "GB": 1024 * 1024 * 1024, "TB": 1024 * 1024 * 1024 * 1024, "PB": 1024 * 1024 * 1024 * 1024 * 1024}
	unit, ok := scale[fields[1]]
	if !ok {
		return 0, false
	}
	return uint64(number*unit + 0.5), true
}

func humanByteLabel(value uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(value)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

func fallbackSize(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
