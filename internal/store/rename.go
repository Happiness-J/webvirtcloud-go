package store

import "context"

func (db *Store) RenameVMReferences(ctx context.Context, hostID, oldName, newName string) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	statements := []string{
		`UPDATE user_vm_access SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE backup_policies SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE metric_samples SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE alert_rules SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE vm_tags SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE vm_schedules SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE ha_vms SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE vm_usage SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
		`UPDATE backup_drills SET vm_name=$3 WHERE host_id=$1 AND vm_name=$2`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, hostID, oldName, newName); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
