package store

import (
	"context"
	"fmt"
	"time"

	"webvirtcloud-go/internal/id"
)

type BackupPolicy struct {
	ID            string
	HostID        string
	VMName        string
	IntervalHours int
	RetainCount   int
	Enabled       bool
	NextRunAt     time.Time
}

func (db *Store) SaveBackupPolicy(ctx context.Context, policy BackupPolicy) error {
	if policy.ID == "" {
		policy.ID = id.New("backup")
	}
	if policy.IntervalHours < 1 || policy.IntervalHours > 168 || policy.RetainCount < 1 || policy.RetainCount > 100 {
		return fmt.Errorf("备份策略参数超出范围")
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO backup_policies(id, host_id, vm_name, interval_hours, retain_count, enabled, next_run_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (host_id, vm_name) DO UPDATE SET interval_hours=EXCLUDED.interval_hours, retain_count=EXCLUDED.retain_count, enabled=EXCLUDED.enabled, next_run_at=EXCLUDED.next_run_at`,
		policy.ID, policy.HostID, policy.VMName, policy.IntervalHours, policy.RetainCount, policy.Enabled, policy.NextRunAt)
	return err
}

func (db *Store) ListDueBackupPolicies(ctx context.Context, now time.Time) ([]BackupPolicy, error) {
	rows, err := db.pool.Query(ctx, `SELECT id, host_id, vm_name, interval_hours, retain_count, enabled, next_run_at FROM backup_policies WHERE enabled AND next_run_at<=$1 ORDER BY next_run_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupPolicy
	for rows.Next() {
		var policy BackupPolicy
		if err := rows.Scan(&policy.ID, &policy.HostID, &policy.VMName, &policy.IntervalHours, &policy.RetainCount, &policy.Enabled, &policy.NextRunAt); err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}

func (db *Store) MarkBackupPolicyRun(ctx context.Context, id string, next time.Time) error {
	_, err := db.pool.Exec(ctx, `UPDATE backup_policies SET next_run_at=$2 WHERE id=$1`, id, next)
	return err
}
