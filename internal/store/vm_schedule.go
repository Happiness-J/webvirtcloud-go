package store

import (
	"context"
	"fmt"
	"time"

	"webvirtcloud-go/internal/id"
)

type VMSchedule struct {
	ID            string
	HostID        string
	VMName        string
	Kind          string
	IntervalHours int
	RetainCount   int
	PowerAction   string
	Enabled       bool
	NextRunAt     time.Time
}

func (db *Store) SaveVMSchedule(ctx context.Context, item VMSchedule) error {
	if item.Kind != "snapshot" && item.Kind != "power" {
		return fmt.Errorf("定时任务类型无效")
	}
	if item.IntervalHours < 1 || item.IntervalHours > 168 || item.RetainCount < 1 || item.RetainCount > 100 {
		return fmt.Errorf("定时参数超出范围")
	}
	if item.Kind == "power" && item.PowerAction != "start" && item.PowerAction != "shutdown" {
		return fmt.Errorf("电源动作无效")
	}
	if item.ID == "" {
		item.ID = id.New("sched")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO vm_schedules(id,host_id,vm_name,kind,interval_hours,retain_count,power_action,enabled,next_run_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (host_id, vm_name, kind) DO UPDATE SET interval_hours=EXCLUDED.interval_hours, retain_count=EXCLUDED.retain_count, power_action=EXCLUDED.power_action, enabled=EXCLUDED.enabled, next_run_at=EXCLUDED.next_run_at`,
		item.ID, item.HostID, item.VMName, item.Kind, item.IntervalHours, item.RetainCount, item.PowerAction, item.Enabled, item.NextRunAt)
	return err
}

func (db *Store) DeleteVMSchedule(ctx context.Context, hostID, vmName, kind string) error {
	if kind != "snapshot" && kind != "power" {
		return fmt.Errorf("定时任务类型无效")
	}
	tag, err := db.pool.Exec(ctx, `DELETE FROM vm_schedules WHERE host_id=$1 AND vm_name=$2 AND kind=$3`, hostID, vmName, kind)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("定时任务不存在")
	}
	return nil
}

func (db *Store) ListDueVMSchedules(ctx context.Context, now time.Time) ([]VMSchedule, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,vm_name,kind,interval_hours,retain_count,power_action,enabled,next_run_at
		FROM vm_schedules WHERE enabled AND next_run_at<=$1 ORDER BY next_run_at`, now)
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

func (db *Store) ListVMSchedules(ctx context.Context, hostID, vmName string) ([]VMSchedule, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,vm_name,kind,interval_hours,retain_count,power_action,enabled,next_run_at
		FROM vm_schedules WHERE host_id=$1 AND vm_name=$2 ORDER BY kind`, hostID, vmName)
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

func (db *Store) MarkVMScheduleRun(ctx context.Context, id string, next time.Time) error {
	_, err := db.pool.Exec(ctx, `UPDATE vm_schedules SET next_run_at=$2 WHERE id=$1`, id, next)
	return err
}
