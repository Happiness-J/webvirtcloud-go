package web

import (
	"strings"
	"time"

	"webvirtcloud-go/internal/libvirt"
)

const backupStaleAfter = 14 * 24 * time.Hour

// backupAge is a read-only view of the newest checkpoint. It never creates
// or restores a backup.
type backupAge struct {
	Status string
	Label  string
	When   string
}

func backupFreshness(items []libvirt.Backup, now time.Time) backupAge {
	if len(items) == 0 {
		return backupAge{Status: "none", Label: "没有检查点"}
	}
	var newest time.Time
	var raw string
	parsed := false
	for _, item := range items {
		when, ok := parseBackupTime(item.CreatedAt)
		if !ok {
			continue
		}
		if !parsed || when.After(newest) {
			newest = when
			raw = item.CreatedAt
			parsed = true
		}
	}
	if !parsed {
		return backupAge{Status: "unknown", Label: "时间读不到", When: items[len(items)-1].CreatedAt}
	}
	age := backupAge{When: raw, Label: "最近 " + raw}
	if now.Sub(newest) > backupStaleAfter {
		age.Status = "stale"
		age.Label = "已超过两周"
		return age
	}
	age.Status = "fresh"
	return age
}

func parseBackupTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if when, err := time.Parse(layout, value); err == nil {
			return when, true
		}
	}
	return time.Time{}, false
}
