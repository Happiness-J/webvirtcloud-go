package store

import (
	"context"
	"fmt"

	"webvirtcloud-go/internal/id"
)

type AlertRule struct {
	ID        string
	HostID    string
	VMName    string
	Metric    string
	Threshold float64
	Enabled   bool
}

func (db *Store) SaveAlertRule(ctx context.Context, rule AlertRule) error {
	if rule.Metric != "cpu" && rule.Metric != "memory" || rule.Threshold <= 0 || rule.Threshold > 100 {
		return fmt.Errorf("告警规则无效")
	}
	if rule.ID == "" {
		rule.ID = id.New("alert")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO alert_rules(id, host_id, vm_name, metric, threshold, enabled) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (host_id, vm_name, metric) DO UPDATE SET threshold=EXCLUDED.threshold, enabled=EXCLUDED.enabled`, rule.ID, rule.HostID, rule.VMName, rule.Metric, rule.Threshold, rule.Enabled)
	return err
}

func (db *Store) ListAlertRules(ctx context.Context, hostID string) ([]AlertRule, error) {
	rows, err := db.pool.Query(ctx, `SELECT id, host_id, vm_name, metric, threshold, enabled FROM alert_rules WHERE host_id=$1 AND enabled ORDER BY vm_name, metric`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var rule AlertRule
		if err := rows.Scan(&rule.ID, &rule.HostID, &rule.VMName, &rule.Metric, &rule.Threshold, &rule.Enabled); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}
