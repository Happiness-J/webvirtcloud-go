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

func (db *Store) IPPoolOnHost(ctx context.Context, hostID, poolID string) (bool, error) {
	var ok bool
	err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ip_pools WHERE host_id=$1 AND id=$2)`, hostID, poolID).Scan(&ok)
	return ok, err
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

func (db *Store) IPPoolName(ctx context.Context, hostID, poolID string) (string, error) {
	var name string
	err := db.pool.QueryRow(ctx, `SELECT name FROM ip_pools WHERE host_id=$1 AND id=$2`, hostID, poolID).Scan(&name)
	return name, err
}

func (db *Store) DeleteIPPool(ctx context.Context, hostID, poolID string) error {
	tag, err := db.pool.Exec(ctx, `DELETE FROM ip_pools WHERE host_id=$1 AND id=$2`, hostID, poolID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("地址池不存在")
	}
	return nil
}

func (db *Store) AllocateIP(ctx context.Context, poolID, owner string) (string, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cidr string
	if err := tx.QueryRow(ctx, `SELECT cidr FROM ip_pools WHERE id=$1 FOR UPDATE`, poolID).Scan(&cidr); err != nil {
		return "", err
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `SELECT address FROM ip_allocations WHERE pool_id=$1`, poolID)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			rows.Close()
			return "", err
		}
		used[address] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	address, ok := nextFreeAddress(prefix, used)
	if !ok {
		return "", fmt.Errorf("地址池已满")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ip_allocations(id,pool_id,address,owner) VALUES($1,$2,$3,$4)`, id.New("ip"), poolID, address.String(), owner); err != nil {
		return "", err
	}
	return address.String(), tx.Commit(ctx)
}

func nextFreeAddress(prefix netip.Prefix, used map[string]bool) (netip.Addr, bool) {
	if !prefix.IsValid() {
		return netip.Addr{}, false
	}
	address := prefix.Addr()
	last := prefixLast(prefix)
	// Ordinary IPv4 networks reserve the first and last addresses.
	// /31 and /32 networks use every address.
	if prefix.Bits() < address.BitLen()-1 {
		address = address.Next()
	}
	for prefix.Contains(address) {
		reservedLast := address == last && prefix.Bits() < address.BitLen()-1
		if !reservedLast && !used[address.String()] {
			return address, true
		}
		next := address.Next()
		if next == address || !prefix.Contains(next) {
			break
		}
		address = next
	}
	return netip.Addr{}, false
}

func prefixLast(prefix netip.Prefix) netip.Addr {
	raw := prefix.Addr().AsSlice()
	hostBits := len(raw)*8 - prefix.Bits()
	for i := len(raw) - 1; i >= 0 && hostBits > 0; i-- {
		bits := hostBits
		if bits > 8 {
			bits = 8
		}
		raw[i] |= 0xff >> (8 - bits)
		hostBits -= bits
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return prefix.Addr()
	}
	return addr
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
