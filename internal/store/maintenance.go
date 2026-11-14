package store

import "context"

func (db *Store) SetHostMaintenance(ctx context.Context, hostID string, enabled bool) error {
	_, err := db.pool.Exec(ctx, `UPDATE hosts SET maintenance=$2, updated_at=now() WHERE id=$1`, hostID, enabled)
	return err
}
