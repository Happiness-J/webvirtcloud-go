package web

// bootMenuOpen marks a VM whose domain XML still enables the boot menu.
// A missing or unreadable definition stays unmarked. The result never
// disables the menu.
func bootMenuOpen(groups []vmGroup, xmlOf func(hostID, name string) (string, error)) map[string]bool {
	out := map[string]bool{}
	for _, group := range groups {
		if group.Err != "" {
			continue
		}
		for _, domain := range group.Domains {
			xmlText, err := xmlOf(group.Host.ID, domain.Name)
			if err != nil {
				continue
			}
			if parseBootInventory(xmlText).BootMenu != "开启" {
				continue
			}
			out[group.Host.ID+"/"+domain.Name] = true
		}
	}
	return out
}
