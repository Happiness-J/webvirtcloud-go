package web

import "webvirtcloud-go/internal/libvirt"

// consoleWithoutPassword names a configured graphics console that has no
// password. A VM with no graphics console stays off the list. The result
// never changes the password or listener.
type consoleWithoutPassword struct {
	Protocol string
	Address  string
}

func consolesWithoutPassword(displays []libvirt.GraphicsDisplay) []consoleWithoutPassword {
	var out []consoleWithoutPassword
	for _, display := range displays {
		protocol := display.Type
		if protocol == "" || display.Password {
			continue
		}
		address := display.Address
		if address == "" {
			address = "127.0.0.1"
		}
		out = append(out, consoleWithoutPassword{Protocol: protocol, Address: address})
	}
	return out
}
