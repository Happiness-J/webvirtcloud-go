package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

func (db *Store) SetVMTag(ctx context.Context, hostID, vmName, tag string, enabled bool) error {
	tag = strings.TrimSpace(tag)
	if err := validTag(tag); err != nil {
		return err
	}
	if enabled {
		_, err := db.pool.Exec(ctx, `INSERT INTO vm_tags(host_id, vm_name, tag) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, hostID, vmName, tag)
		return err
	}
	_, err := db.pool.Exec(ctx, `DELETE FROM vm_tags WHERE host_id=$1 AND vm_name=$2 AND tag=$3`, hostID, vmName, tag)
	return err
}

func validTag(tag string) error {
	if tag == "" || len([]rune(tag)) > 32 || strings.ContainsAny(tag, " \t\r\n") {
		return fmt.Errorf("标签需要 1 到 32 个字符，不能含空格")
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return fmt.Errorf("标签包含无效字符")
		}
	}
	return nil
}

func (db *Store) ListVMTags(ctx context.Context, hostID string) (map[string][]string, error) {
	rows, err := db.pool.Query(ctx, `SELECT vm_name, tag FROM vm_tags WHERE host_id=$1 ORDER BY vm_name, tag`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var name, tag string
		if err := rows.Scan(&name, &tag); err != nil {
			return nil, err
		}
		out[name] = append(out[name], tag)
	}
	return out, rows.Err()
}

func (db *Store) VMsByTag(ctx context.Context, hostID, tag string) (map[string]bool, error) {
	rows, err := db.pool.Query(ctx, `SELECT vm_name FROM vm_tags WHERE host_id=$1 AND tag=$2`, hostID, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
