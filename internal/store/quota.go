package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"webvirtcloud-go/internal/id"
)

type UserQuota struct {
	UserID      string
	MaxVMs      int
	MaxVCPUs    int
	MaxMemoryGB int
	MaxDiskGB   int
}

type Usage struct {
	VMs      int
	VCPUs    int
	MemoryGB int
	DiskGB   int
}

func (db *Store) SaveUserQuota(ctx context.Context, quota UserQuota) error {
	if quota.MaxVMs < 0 || quota.MaxVCPUs < 0 || quota.MaxMemoryGB < 0 || quota.MaxDiskGB < 0 {
		return fmt.Errorf("配额不能为负数")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO user_quotas(user_id,max_vms,max_vcpus,max_memory_gb,max_disk_gb)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET max_vms=EXCLUDED.max_vms, max_vcpus=EXCLUDED.max_vcpus, max_memory_gb=EXCLUDED.max_memory_gb, max_disk_gb=EXCLUDED.max_disk_gb`,
		quota.UserID, quota.MaxVMs, quota.MaxVCPUs, quota.MaxMemoryGB, quota.MaxDiskGB)
	return err
}

func (db *Store) GetUserQuota(ctx context.Context, userID string) (UserQuota, error) {
	var quota UserQuota
	err := db.pool.QueryRow(ctx, `SELECT user_id,max_vms,max_vcpus,max_memory_gb,max_disk_gb FROM user_quotas WHERE user_id=$1`, userID).
		Scan(&quota.UserID, &quota.MaxVMs, &quota.MaxVCPUs, &quota.MaxMemoryGB, &quota.MaxDiskGB)
	if err == pgx.ErrNoRows {
		return UserQuota{UserID: userID, MaxVMs: 20, MaxVCPUs: 64, MaxMemoryGB: 256, MaxDiskGB: 2000}, nil
	}
	return quota, err
}

func (db *Store) ReserveVMUsage(ctx context.Context, userID, hostID, vmName string, vcpus, memoryGB, diskGB int) error {
	quota, err := db.GetUserQuota(ctx, userID)
	if err != nil {
		return err
	}
	usage, err := db.UserUsage(ctx, userID)
	if err != nil {
		return err
	}
	if usage.VMs+1 > quota.MaxVMs || usage.VCPUs+vcpus > quota.MaxVCPUs || usage.MemoryGB+memoryGB > quota.MaxMemoryGB || usage.DiskGB+diskGB > quota.MaxDiskGB {
		return fmt.Errorf("超出用户资源配额")
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO vm_usage(id,user_id,host_id,vm_name,vcpus,memory_gb,disk_gb)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, id.New("use"), userID, hostID, vmName, vcpus, memoryGB, diskGB)
	return err
}

func (db *Store) UserUsage(ctx context.Context, userID string) (Usage, error) {
	var usage Usage
	err := db.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(vcpus),0), COALESCE(sum(memory_gb),0), COALESCE(sum(disk_gb),0)
		FROM vm_usage WHERE user_id=$1`, userID).Scan(&usage.VMs, &usage.VCPUs, &usage.MemoryGB, &usage.DiskGB)
	return usage, err
}
