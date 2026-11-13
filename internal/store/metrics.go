package store

import (
	"context"
	"time"
)

type MetricSample struct {
	HostID        string
	VMName        string
	CPUPercent    float64
	MemoryPercent float64
	DiskReadBps   int64
	DiskWriteBps  int64
	SampledAt     time.Time
}

func (db *Store) AddMetricSample(ctx context.Context, sample MetricSample) error {
	_, err := db.pool.Exec(ctx, `INSERT INTO metric_samples(host_id, vm_name, cpu_percent, memory_percent, disk_read_bps, disk_write_bps, sampled_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, sample.HostID, sample.VMName, sample.CPUPercent, sample.MemoryPercent, sample.DiskReadBps, sample.DiskWriteBps, sample.SampledAt)
	return err
}

func (db *Store) ListMetricSamples(ctx context.Context, hostID, vmName string, since time.Time) ([]MetricSample, error) {
	rows, err := db.pool.Query(ctx, `SELECT host_id, vm_name, cpu_percent, memory_percent, disk_read_bps, disk_write_bps, sampled_at FROM metric_samples WHERE host_id=$1 AND vm_name=$2 AND sampled_at>=$3 ORDER BY sampled_at`, hostID, vmName, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricSample
	for rows.Next() {
		var sample MetricSample
		if err := rows.Scan(&sample.HostID, &sample.VMName, &sample.CPUPercent, &sample.MemoryPercent, &sample.DiskReadBps, &sample.DiskWriteBps, &sample.SampledAt); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}
