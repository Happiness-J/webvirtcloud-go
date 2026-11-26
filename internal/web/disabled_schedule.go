package web

import "webvirtcloud-go/internal/store"

// disabledSchedule is a saved snapshot or power schedule that is turned off.
// The list never enables or deletes it.
type disabledSchedule struct {
	HostID   string
	HostName string
	VMName   string
	Label    string
}

func disabledSchedules(items []store.VMSchedule, hosts []store.Host) []disabledSchedule {
	names := map[string]string{}
	for _, host := range hosts {
		names[host.ID] = host.Name
	}
	var out []disabledSchedule
	for _, item := range items {
		if item.Enabled || (item.Kind != "snapshot" && item.Kind != "power") {
			continue
		}
		hostName, ok := names[item.HostID]
		if !ok {
			continue
		}
		out = append(out, disabledSchedule{HostID: item.HostID, HostName: hostName, VMName: item.VMName, Label: scheduleLabel(item)})
	}
	return out
}
