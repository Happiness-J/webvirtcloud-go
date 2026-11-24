package store

import (
	"context"
	"testing"
	"time"
)

func TestHostCapacityRange(t *testing.T) {
	db := &Store{}
	if err := db.RecordHostCapacity(context.TODO(), HostCapacitySample{HostID: "h", CPUPercent: 101}); err == nil {
		t.Fatal("cpu over 100 accepted")
	}
	if err := db.RecordHostCapacity(context.TODO(), HostCapacitySample{CPUPercent: 10}); err == nil {
		t.Fatal("empty host accepted")
	}
}

func TestHostCapacityRetentionAndHistory(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	host := createTestHost(t, db)
	if err := db.RecordHostCapacity(ctx, HostCapacitySample{HostID: host.ID, CPUPercent: 12, MemPercent: 34, RunningVMs: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO host_capacity_samples(host_id,cpu_percent,mem_percent,running_vms,sampled_at) VALUES($1,90,90,9,$2)`, host.ID, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	removed, err := db.PruneHostCapacity(ctx, time.Now().Add(-HostCapacityRetention))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("pruned %d, want 1", removed)
	}
	rows, err := db.ListHostCapacity(ctx, host.ID, time.Now().Add(-HostCapacityRetention))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CPUPercent != 12 || rows[0].MemPercent != 34 || rows[0].RunningVMs != 2 {
		t.Fatalf("history = %#v", rows)
	}
}
