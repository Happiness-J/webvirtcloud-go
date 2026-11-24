package store

import (
	"context"
	"fmt"
	"time"
)

type ClusterPolicy struct {
	Enabled   bool
	Policy    string
	FailAfter int
	UpdatedAt time.Time
}

type HostHealth struct {
	HostID     string
	Reachable  bool
	Failures   int
	CPUPercent float64
	MemPercent float64
	RunningVMs int
	CheckedAt  time.Time
}

type HAVM struct {
	HostID    string
	VMName    string
	Protected bool
	UpdatedAt time.Time
}

func (db *Store) ClusterPolicy(ctx context.Context) (ClusterPolicy, error) {
	var item ClusterPolicy
	err := db.pool.QueryRow(ctx, `SELECT enabled, policy, fail_after, updated_at FROM cluster_policy WHERE id=1`).
		Scan(&item.Enabled, &item.Policy, &item.FailAfter, &item.UpdatedAt)
	return item, err
}

func (db *Store) SaveClusterPolicy(ctx context.Context, item ClusterPolicy) error {
	switch item.Policy {
	case "spread", "pack", "balanced":
	default:
		return fmt.Errorf("放置策略无效")
	}
	if item.FailAfter < 2 || item.FailAfter > 10 {
		return fmt.Errorf("故障确认次数必须在 2 到 10 之间")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO cluster_policy(id, enabled, policy, fail_after)
		VALUES(1,$1,$2,$3)
		ON CONFLICT (id) DO UPDATE SET enabled=EXCLUDED.enabled, policy=EXCLUDED.policy, fail_after=EXCLUDED.fail_after, updated_at=now()`,
		item.Enabled, item.Policy, item.FailAfter)
	return err
}

func (db *Store) RecordHostHealth(ctx context.Context, item HostHealth) error {
	if item.Failures < 0 || item.RunningVMs < 0 || item.CPUPercent < 0 || item.CPUPercent > 100 || item.MemPercent < 0 || item.MemPercent > 100 {
		return fmt.Errorf("主机健康数据超出范围")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO host_health(host_id,reachable,failures,cpu_percent,mem_percent,running_vms,checked_at)
		VALUES($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (host_id) DO UPDATE SET reachable=EXCLUDED.reachable, failures=EXCLUDED.failures,
			cpu_percent=EXCLUDED.cpu_percent, mem_percent=EXCLUDED.mem_percent, running_vms=EXCLUDED.running_vms, checked_at=now()`,
		item.HostID, item.Reachable, item.Failures, item.CPUPercent, item.MemPercent, item.RunningVMs)
	return err
}

func (db *Store) ListHostHealth(ctx context.Context) ([]HostHealth, error) {
	rows, err := db.pool.Query(ctx, `SELECT host_id,reachable,failures,cpu_percent,mem_percent,running_vms,checked_at FROM host_health`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostHealth
	for rows.Next() {
		var item HostHealth
		if err := rows.Scan(&item.HostID, &item.Reachable, &item.Failures, &item.CPUPercent, &item.MemPercent, &item.RunningVMs, &item.CheckedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *Store) SaveHAVM(ctx context.Context, item HAVM) error {
	if item.HostID == "" || item.VMName == "" {
		return fmt.Errorf("高可用虚拟机参数无效")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO ha_vms(host_id,vm_name,protected)
		VALUES($1,$2,$3)
		ON CONFLICT (host_id, vm_name) DO UPDATE SET protected=EXCLUDED.protected, updated_at=now()`,
		item.HostID, item.VMName, item.Protected)
	return err
}

func (db *Store) ListHAVMs(ctx context.Context, hostID string) ([]HAVM, error) {
	rows, err := db.pool.Query(ctx, `SELECT host_id,vm_name,protected,updated_at FROM ha_vms WHERE host_id=$1 ORDER BY vm_name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HAVM
	for rows.Next() {
		var item HAVM
		if err := rows.Scan(&item.HostID, &item.VMName, &item.Protected, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *Store) ListProtectedVMs(ctx context.Context) ([]HAVM, error) {
	rows, err := db.pool.Query(ctx, `SELECT host_id,vm_name,protected,updated_at FROM ha_vms WHERE protected ORDER BY host_id, vm_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HAVM
	for rows.Next() {
		var item HAVM
		if err := rows.Scan(&item.HostID, &item.VMName, &item.Protected, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

const HostCapacityRetention = 24 * time.Hour

// HostCapacitySample is one read-only capacity observation. host_health keeps
// only the latest probe, so trends need their own append-only history.
type HostCapacitySample struct {
	HostID     string
	CPUPercent float64
	MemPercent float64
	RunningVMs int
	SampledAt  time.Time
}

func (db *Store) RecordHostCapacity(ctx context.Context, item HostCapacitySample) error {
	if item.HostID == "" || item.RunningVMs < 0 || item.CPUPercent < 0 || item.CPUPercent > 100 || item.MemPercent < 0 || item.MemPercent > 100 {
		return fmt.Errorf("主机容量数据超出范围")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO host_capacity_samples(host_id,cpu_percent,mem_percent,running_vms,sampled_at)
		VALUES($1,$2,$3,$4,now())`, item.HostID, item.CPUPercent, item.MemPercent, item.RunningVMs)
	return err
}

func (db *Store) ListHostCapacity(ctx context.Context, hostID string, since time.Time) ([]HostCapacitySample, error) {
	rows, err := db.pool.Query(ctx, `SELECT host_id,cpu_percent,mem_percent,running_vms,sampled_at
		FROM host_capacity_samples WHERE host_id=$1 AND sampled_at>=$2 ORDER BY sampled_at`, hostID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostCapacitySample
	for rows.Next() {
		var item HostCapacitySample
		if err := rows.Scan(&item.HostID, &item.CPUPercent, &item.MemPercent, &item.RunningVMs, &item.SampledAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// PruneHostCapacity drops samples older than the retention window so the
// append-only history cannot grow without a bound.
func (db *Store) PruneHostCapacity(ctx context.Context, before time.Time) (int64, error) {
	tag, err := db.pool.Exec(ctx, `DELETE FROM host_capacity_samples WHERE sampled_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
