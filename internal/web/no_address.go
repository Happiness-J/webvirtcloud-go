package web

// noAddressFromProbe marks a running VM whose guest agent answered but no
// guest address was read. A failed agent probe is left to the existing
// no-agent marker. The result never changes a network.
func noAddressFromProbe(groups []vmGroup, answered func(hostID, name string) bool, hasAddress func(hostID, name string) bool) map[string]bool {
	out := map[string]bool{}
	for _, group := range groups {
		if group.Err != "" {
			continue
		}
		for _, domain := range group.Domains {
			if domain.State != "running" {
				continue
			}
			if !answered(group.Host.ID, domain.Name) || hasAddress(group.Host.ID, domain.Name) {
				continue
			}
			out[group.Host.ID+"/"+domain.Name] = true
		}
	}
	return out
}
