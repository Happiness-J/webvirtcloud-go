package jobs

import (
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestEventForFailedJob(t *testing.T) {
	if got := eventForFailedJob(&store.Job{Kind: "backup-policy", ArgsJSON: `["checkpoint-create-as","web"]`}); got != "backup.failed" {
		t.Fatalf("backup = %s", got)
	}
	if got := eventForFailedJob(&store.Job{Kind: "vm-action", ArgsJSON: `["destroy","web"]`}); got != "vm.forced_off" {
		t.Fatalf("destroy = %s", got)
	}
	if got := eventForFailedJob(&store.Job{Kind: "vm-action", ArgsJSON: `["shutdown","web"]`}); got != "" {
		t.Fatalf("shutdown = %s", got)
	}
}
