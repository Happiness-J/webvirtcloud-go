package web

import (
	"strings"

	"webvirtcloud-go/internal/store"
)

// failedStart is a VM whose newest start job failed and no later start job
// succeeded. The list never retries a job.
type failedStart struct {
	HostID   string
	HostName string
	VMName   string
}

func latestFailedStarts(jobs []store.Job) []failedStart {
	seen := map[string]bool{}
	var out []failedStart
	for _, job := range jobs {
		name := startVMName(job)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if job.Status != "failed" {
			continue
		}
		out = append(out, failedStart{HostID: job.HostID, HostName: job.HostName, VMName: name})
	}
	return out
}

func startVMName(job store.Job) string {
	label := strings.TrimSpace(job.Label)
	if strings.Contains(label, "敏感参数已隐藏") {
		return ""
	}
	name, action, ok := strings.Cut(label, ":")
	if !ok || strings.TrimSpace(action) != "start" || !validTargetToken(strings.TrimSpace(name)) {
		return ""
	}
	return strings.TrimSpace(name)
}
