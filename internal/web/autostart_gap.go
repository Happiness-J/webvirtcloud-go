package web

import "strings"

// autostartGap reports a read-only mismatch between power state and the
// autostart flag. It never changes the flag.
type autostartGap struct {
	Mismatch bool
	Label    string
}

func autostartGapFor(state, autostart string) autostartGap {
	enabled := autostartEnabled(autostart)
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "shut off", "shutoff", "stopped", "inactive":
		if enabled {
			return autostartGap{true, "该开未开"}
		}
	case "running", "paused":
		if !enabled {
			return autostartGap{true, "关机后不拉起"}
		}
	}
	return autostartGap{}
}

func autostartEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enable", "enabled", "yes", "on", "true":
		return true
	default:
		return false
	}
}
