package web

import (
	"time"

	"webvirtcloud-go/internal/store"
)

const queueWaitLimit = 15 * time.Minute

// longQueuedJob is a job still waiting after the wait limit. The list is
// read-only and never cancels or retries a job.
type longQueuedJob struct {
	ID    string
	Label string
	Host  string
	Wait  string
}

func longQueuedJobs(jobs []store.Job, now time.Time) []longQueuedJob {
	var out []longQueuedJob
	for _, job := range jobs {
		if job.Status != "queued" || job.CreatedAt.IsZero() {
			continue
		}
		waited := now.Sub(job.CreatedAt)
		if waited < queueWaitLimit {
			continue
		}
		out = append(out, longQueuedJob{ID: job.ID, Label: job.Label, Host: job.HostName, Wait: formatQueueWait(waited)})
	}
	return out
}

func formatQueueWait(waited time.Duration) string {
	minutes := int(waited.Minutes())
	if minutes < 60 {
		return "已等 15 分钟以上"
	}
	return "已等 1 小时以上"
}
