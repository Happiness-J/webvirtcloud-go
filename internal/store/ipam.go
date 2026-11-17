package store

import (
	"context"
	"fmt"
	"net/netip"

	"webvirtcloud-go/internal/id"
)

type IPPool struct {
	ID       string
	HostID   string
	Name     string
	CIDR     string
	Floating bool
}

type IPAllocation struct {
	ID      string
	PoolID  string
	Address string
	Owner   string
}

func (db *Store) CreateIPPool(ctx context.Context, pool IPPool) error {
	if _, err := netip.ParsePrefix(pool.CIDR); err != nil {
		return fmt.Errorf("地址池网段无效")
	}
	if pool.Name == "" || len(pool.Name) > 64 {
		return fmt.Errorf("地址池名称无效")
	}
	if pool.ID == "" {
		pool.ID = id.New("pool")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO ip_pools(id,host_id,name,cidr,floating) VALUES($1,$2,$3,$4,$5)`, pool.ID, pool.HostID, pool.Name, pool.CIDR, pool.Floating)
	return err
}

func (db *Store) ListIPPools(ctx context.Context, hostID string) ([]IPPool, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,name,cidr,floating FROM ip_pools WHERE host_id=$1 ORDER BY name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPPool
	for rows.Next() {
		var item IPPool
		if err := rows.Scan(&item.ID, &item.HostID, &item.Name, &item.CIDR, &item.Floating); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *Store) AllocateIP(ctx context.Context, poolID, owner string) (string, error) {
	var cidr string
	if err := db.pool.QueryRow(ctx, `SELECT cidr FROM ip_pools WHERE id=$1`, poolID).Scan(&cidr); err != nil {
		return "", err
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", err
	}
	rows, err := db.pool.Query(ctx, `SELECT address FROM ip_allocations WHERE pool_id=$1`, poolID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := map[string]bool{}
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return "", err
		}
		used[address] = true
	}
	address := prefix.Addr().Next()
	for prefix.Contains(address) && address != prefix.Addr() {
		if !used[address.String()] && address != prefix.Masked().Addr() {
			_, err = db.pool.Exec(ctx, `INSERT INTO ip_allocations(id,pool_id,address,owner) VALUES($1,$2,$3,$4)`, id.New("ip"), poolID, address.String(), owner)
			return address.String(), err
		}
		next := address.Next()
		if next == address {
			break
		}
		address = next
		if len(used) > 4096 {
			break
		}
	}
	return "", fmt.Errorf("地址池已满")
}

func (db *Store) ReleaseIP(ctx context.Context, poolID, address string) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM ip_allocations WHERE pool_id=$1 AND address=$2`, poolID, address)
	return err
}

func (db *Store) ListIPAllocations(ctx context.Context, poolID string) ([]IPAllocation, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,pool_id,address,owner FROM ip_allocations WHERE pool_id=$1 ORDER BY address`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPAllocation
	for rows.Next() {
		var item IPAllocation
		if err := rows.Scan(&item.ID, &item.PoolID, &item.Address, &item.Owner); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
