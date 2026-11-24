package web

import "webvirtcloud-go/internal/libvirt"
import "webvirtcloud-go/internal/store"

// maintenanceImpact is a read-only view shown before entering maintenance or
// evacuating a host. It never starts migration or changes a job.
type maintenanceImpact struct {
	Running     []libvirt.Domain
	Autostart   []libvirt.Domain
	OpenJobs    []store.Job
	Unavailable bool
}

func buildMaintenanceImpact(domains []libvirt.Domain, jobs []store.Job, unavailable bool) maintenanceImpact {
	impact := maintenanceImpact{Unavailable: unavailable}
	for _, domain := range domains {
		if domain.State == "running" || domain.State == "paused" {
			impact.Running = append(impact.Running, domain)
		}
		if domain.Autostart == "yes" || domain.Autostart == "enable" {
			impact.Autostart = append(impact.Autostart, domain)
		}
	}
	for _, job := range jobs {
		if job.Status == "queued" || job.Status == "running" {
			impact.OpenJobs = append(impact.OpenJobs, job)
		}
	}
	return impact
}
