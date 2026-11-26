package store

import "context"

func (db *Store) ListDisabledVMSchedules(ctx context.Context) ([]VMSchedule, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,vm_name,kind,interval_hours,retain_count,power_action,enabled,next_run_at
		FROM vm_schedules WHERE NOT enabled ORDER BY host_id, vm_name, kind`)
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
