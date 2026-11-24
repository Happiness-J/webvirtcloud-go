package web

// idleVM is a shut-off VM that will not start with its host. The list is
// read-only and never starts or deletes a VM. libvirt does not report how
// long a domain has been shut off, so the list does not claim an age.
type idleVM struct {
	HostID   string
	HostName string
	Name     string
}

func idleVMs(groups []vmGroup) []idleVM {
	var out []idleVM
	for _, group := range groups {
		if group.Err != "" {
			continue
		}
		for _, domain := range group.Domains {
			if !shutOff(domain.State) || autostartEnabled(domain.Autostart) {
				continue
			}
			out = append(out, idleVM{HostID: group.Host.ID, HostName: group.Host.Name, Name: domain.Name})
		}
	}
	return out
}

func shutOff(state string) bool {
	switch state {
	case "shut off", "shutoff":
		return true
	default:
		return false
	}
}
