package web

import "webvirtcloud-go/internal/libvirt"

// exposedConsole reports a running VM whose graphics listener is not
// loopback or has no password. It never changes the listener or password.
func consoleExposed(state string, displays []libvirt.GraphicsDisplay) bool {
	if state != "running" {
		return false
	}
	for _, display := range displays {
		if len(consoleSecurityWarnings(libvirt.Console{Type: display.Type, Address: display.Address, Password: passwordMarker(display.Password)})) > 0 {
			return true
		}
	}
	return false
}

func passwordMarker(set bool) string {
	if set {
		return "set"
	}
	return ""
}
