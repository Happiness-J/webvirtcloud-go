package web

import (
	"time"

	"webvirtcloud-go/internal/store"
)

const scheduleDueWithin = 7 * 24 * time.Hour

// dueSchedule is an enabled snapshot or power schedule whose next run is
// inside the next seven days. The list is display only and never runs a job.
type dueSchedule struct {
	HostID   string
	HostName string
	VMName   string
	Label    string
	When     string
}

func schedulesDueSoon(items []store.VMSchedule, hosts []store.Host, now time.Time) []dueSchedule {
	names := map[string]string{}
	for _, host := range hosts {
		names[host.ID] = host.Name
	}
	until := now.Add(scheduleDueWithin)
	var out []dueSchedule
	for _, item := range items {
		if !item.Enabled || item.NextRunAt.IsZero() || item.NextRunAt.After(until) {
			continue
		}
		if item.Kind != "snapshot" && item.Kind != "power" {
			continue
		}
		hostName, ok := names[item.HostID]
		if !ok {
			continue
		}
		out = append(out, dueSchedule{
			HostID: item.HostID, HostName: hostName, VMName: item.VMName,
			Label: scheduleLabel(item), When: scheduleWhen(item.NextRunAt, now),
		})
	}
	return out
}

func scheduleLabel(item store.VMSchedule) string {
	if item.Kind == "snapshot" {
		return "定时快照"
	}
	if item.PowerAction == "start" {
		return "定时开机"
	}
	return "定时关机"
}

func scheduleWhen(next, now time.Time) string {
	if !next.After(now) {
		return "已到时间"
	}
	hours := int(next.Sub(now).Hours())
	if hours < 24 {
		return "一天内"
	}
	return "七天内"
}
