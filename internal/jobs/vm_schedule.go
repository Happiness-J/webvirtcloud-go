package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/store"
)

func (w *Worker) enqueueDueVMSchedules(ctx context.Context) {
	if err := w.store.EnsureSystemUser(ctx, store.User{ID: "system:schedule", Username: "system:schedule", PasswordHash: "disabled", Role: "operator"}); err != nil {
		w.log.Warn("ensure schedule user", "error", err)
		return
	}
	items, err := w.store.ListDueVMSchedules(ctx, time.Now())
	if err != nil {
		w.log.Warn("list vm schedules", "error", err)
		return
	}
	for _, item := range items {
		args, label, kind := scheduleJob(item)
		raw, _ := json.Marshal(args)
		job := store.Job{ID: id.New("job"), UserID: "system:schedule", HostID: item.HostID, Kind: kind, Label: label, Tool: "virsh", ArgsJSON: string(raw), TimeoutSeconds: 900}
		if err := w.store.EnqueueJob(ctx, job); err != nil {
			w.log.Warn("enqueue vm schedule", "schedule", item.ID, "error", err)
			continue
		}
		if err := w.store.MarkVMScheduleRun(ctx, item.ID, time.Now().Add(time.Duration(item.IntervalHours)*time.Hour)); err != nil {
			w.log.Warn("advance vm schedule", "schedule", item.ID, "error", err)
		}
	}
}

func scheduleJob(item store.VMSchedule) ([]string, string, string) {
	switch item.Kind {
	case "snapshot":
		name := fmt.Sprintf("auto-%s", time.Now().UTC().Format("20060102-150405"))
		return []string{"snapshot-create-as", item.VMName, name, "--quiesce"}, fmt.Sprintf("定时快照 %s · 保留 %d", item.VMName, item.RetainCount), "vm-action"
	default:
		return []string{item.PowerAction, item.VMName}, "定时电源 " + item.VMName, "vm-action"
	}
}
