package store

import (
	"context"
	"time"
)

func (db *Store) ListVMSchedulesDueBefore(ctx context.Context, until time.Time) ([]VMSchedule, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,vm_name,kind,interval_hours,retain_count,power_action,enabled,next_run_at
		FROM vm_schedules WHERE enabled AND next_run_at<=$1 ORDER BY next_run_at, host_id, vm_name, kind`, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VMSchedule
	for rows.Next() {
		var item VMSchedule
		if err := rows.Scan(&item.ID, &item.HostID, &item.VMName, &item.Kind, &item.IntervalHours, &item.RetainCount, &item.PowerAction, &item.Enabled, &item.NextRunAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
