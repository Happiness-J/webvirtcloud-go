package web

import "strings"

// pausedVM is a VM whose memory is still held because it is paused.
// The list is read-only and never resumes or stops a VM.
type pausedVM struct {
	HostID   string
	HostName string
	Name     string
}

func pausedVMs(groups []vmGroup) []pausedVM {
	var out []pausedVM
	for _, group := range groups {
		if group.Err != "" {
			continue
		}
		for _, domain := range group.Domains {
			if !strings.EqualFold(strings.TrimSpace(domain.State), "paused") {
				continue
			}
			out = append(out, pausedVM{HostID: group.Host.ID, HostName: group.Host.Name, Name: domain.Name})
		}
	}
	return out
}
