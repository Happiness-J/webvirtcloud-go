package web

import "webvirtcloud-go/internal/libvirt"

func backupChainReady(items []libvirt.Backup) bool {
	seen := map[string]bool{}
	for _, item := range items {
		if item.Name == "" || seen[item.Name] {
			return false
		}
		seen[item.Name] = true
	}
	for _, item := range items {
		if item.Parent != "" && !seen[item.Parent] {
			return false
		}
	}
	return true
}
