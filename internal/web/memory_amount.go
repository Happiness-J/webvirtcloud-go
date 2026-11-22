package web

import (
	"strconv"
	"strings"
)

func memoryAmount(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 || !strings.EqualFold(fields[len(fields)-1], "KiB") {
		return "4G"
	}
	kiB, err := strconv.ParseUint(strings.ReplaceAll(fields[0], ",", ""), 10, 64)
	if err != nil || kiB == 0 {
		return "4G"
	}
	const (
		miB = 1024
		giB = 1024 * 1024
	)
	switch {
	case kiB%giB == 0:
		return strconv.FormatUint(kiB/giB, 10) + "G"
	case kiB%miB == 0:
		return strconv.FormatUint(kiB/miB, 10) + "M"
	default:
		return strconv.FormatUint(kiB, 10) + "K"
	}
}
