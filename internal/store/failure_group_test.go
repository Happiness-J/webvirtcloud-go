package store

import (
	"context"
	"testing"
	"time"

	"webvirtcloud-go/internal/id"
)

func TestListFailedJobsDecryptsStoredError(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "vm-action", Label: "创建虚拟机", Tool: "virt-install",
		ArgsJSON: `["--name","demo"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJob(ctx, "worker-a")
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	if finished, err := db.FinishJob(ctx, claimed.ID, "worker-a", "failed", "", "虚拟机名称无效"); err != nil || !finished {
		t.Fatalf("finish = %v %v", finished, err)
	}
	rows, err := db.ListFailedJobs(ctx, user.ID, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Error != "虚拟机名称无效" || rows[0].Label != "创建虚拟机" {
		t.Fatalf("failed jobs = %#v", rows)
	}
	var plain string
	if err := db.pool.QueryRow(ctx, `SELECT error FROM jobs WHERE id=$1`, jobID).Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if plain != "" {
		t.Fatal("failure text stored in plaintext")
	}
}
