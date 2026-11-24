package store

import (
	"context"
	"fmt"

	"webvirtcloud-go/internal/id"
)

type VMTemplate struct {
	ID        string
	Name      string
	VCPUs     int
	MemoryGB  int
	DiskGB    int
	Network   string
	OSVariant string
	Hostname  string
	Username  string
	Address   string
	Gateway   string
	DNS       string
}

func (db *Store) SaveVMTemplate(ctx context.Context, item VMTemplate) error {
	if item.Name == "" || len(item.Name) > 64 || item.VCPUs < 1 || item.MemoryGB < 1 || item.DiskGB < 1 {
		return fmt.Errorf("模板参数无效")
	}
	if item.ID == "" {
		item.ID = id.New("template")
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO vm_templates(id,name,vcpus,memory_gb,disk_gb,network,os_variant,hostname,username,address,gateway,dns)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(name) DO UPDATE SET vcpus=EXCLUDED.vcpus, memory_gb=EXCLUDED.memory_gb, disk_gb=EXCLUDED.disk_gb,
		network=EXCLUDED.network, os_variant=EXCLUDED.os_variant, hostname=EXCLUDED.hostname, username=EXCLUDED.username,
		address=EXCLUDED.address, gateway=EXCLUDED.gateway, dns=EXCLUDED.dns`,
		item.ID, item.Name, item.VCPUs, item.MemoryGB, item.DiskGB, item.Network, item.OSVariant, item.Hostname, item.Username, item.Address, item.Gateway, item.DNS)
	return err
}

func (db *Store) ListVMTemplates(ctx context.Context) ([]VMTemplate, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,name,vcpus,memory_gb,disk_gb,network,os_variant,hostname,username,address,gateway,dns FROM vm_templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VMTemplate
	for rows.Next() {
		var item VMTemplate
		if err := rows.Scan(&item.ID, &item.Name, &item.VCPUs, &item.MemoryGB, &item.DiskGB, &item.Network, &item.OSVariant, &item.Hostname, &item.Username, &item.Address, &item.Gateway, &item.DNS); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
