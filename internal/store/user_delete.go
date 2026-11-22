package store

import "context"

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminLock); err != nil {
		return err
	}
	var role string
	var disabled bool
	if err := tx.QueryRow(ctx, `SELECT role, disabled FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&role, &disabled); err != nil {
		return err
	}
	if role == "admin" && !disabled {
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=false AND id<>$1`, id).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
