package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/store"
)

func (w *Worker) enqueueDueBackups(ctx context.Context) {
	policies, err := w.store.ListDueBackupPolicies(ctx, time.Now())
	if err != nil {
		w.log.Warn("list backup policies", "error", err)
		return
	}
	for _, policy := range policies {
		name := fmt.Sprintf("auto-%s", time.Now().UTC().Format("20060102-150405"))
		args, _ := json.Marshal([]string{"checkpoint-create-as", policy.VMName, "--name", name, "--retain", fmt.Sprintf("%d", policy.RetainCount)})
		job := store.Job{ID: id.New("job"), UserID: "system:backup", HostID: policy.HostID, Kind: "backup-policy", Label: "定时检查点 " + policy.VMName, Tool: "virsh", ArgsJSON: string(args), TimeoutSeconds: 1800}
		if err := w.store.EnqueueJob(ctx, job); err != nil {
			w.log.Warn("enqueue scheduled backup", "policy", policy.ID, "error", err)
			continue
		}
		if err := w.store.MarkBackupPolicyRun(ctx, policy.ID, time.Now().Add(time.Duration(policy.IntervalHours)*time.Hour)); err != nil {
			w.log.Warn("advance backup policy", "policy", policy.ID, "error", err)
		}
	}
}
