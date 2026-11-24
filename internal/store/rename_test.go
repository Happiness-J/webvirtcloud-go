package store

import (
	"context"
	"testing"
	"time"

	"webvirtcloud-go/internal/id"
)

func TestRenameVMReferencesMovesAllHostRecords(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	host := createTestHost(t, db)
	user := createTestUser(t, db, "operator")
	const oldName, newName = "web-old", "web-new"

	if err := db.GrantVMAccess(ctx, user.ID, host.ID, oldName); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBackupPolicy(ctx, BackupPolicy{HostID: host.ID, VMName: oldName, IntervalHours: 24, RetainCount: 3, Enabled: true, NextRunAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddMetricSample(ctx, MetricSample{HostID: host.ID, VMName: oldName, CPUPercent: 1, SampledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAlertRule(ctx, AlertRule{HostID: host.ID, VMName: oldName, Metric: "cpu", Threshold: 80, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetVMTag(ctx, host.ID, oldName, "prod", true); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveVMSchedule(ctx, VMSchedule{HostID: host.ID, VMName: oldName, Kind: "snapshot", IntervalHours: 12, RetainCount: 2, Enabled: true, NextRunAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHAVM(ctx, HAVM{HostID: host.ID, VMName: oldName, Protected: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReserveVMUsage(ctx, user.ID, host.ID, oldName, 1, 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.AddBackupDrill(ctx, BackupDrill{ID: id.New("drill"), HostID: host.ID, VMName: oldName, TargetKind: "local", Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Result: "passed", ActorName: user.Username}); err != nil {
		t.Fatal(err)
	}

	if err := db.RenameVMReferences(ctx, host.ID, oldName, newName); err != nil {
		t.Fatal(err)
	}

	access, err := db.ListVMAccess(ctx, user.ID)
	if err != nil || len(access) != 1 || access[0].VMName != newName {
		t.Fatalf("access = %+v, %v", access, err)
	}
	policies, err := db.ListDueBackupPolicies(ctx, time.Now().Add(2*time.Hour))
	if err != nil || len(policies) != 1 || policies[0].VMName != newName {
		t.Fatalf("policies = %+v, %v", policies, err)
	}
	samples, err := db.ListMetricSamples(ctx, host.ID, newName, time.Now().Add(-time.Hour))
	if err != nil || len(samples) != 1 {
		t.Fatalf("samples = %+v, %v", samples, err)
	}
	rules, err := db.ListAlertRules(ctx, host.ID)
	if err != nil || len(rules) != 1 || rules[0].VMName != newName {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	tags, err := db.ListVMTags(ctx, host.ID)
	if err != nil || len(tags[newName]) != 1 || len(tags[oldName]) != 0 {
		t.Fatalf("tags = %+v, %v", tags, err)
	}
	schedules, err := db.ListVMSchedules(ctx, host.ID, newName)
	if err != nil || len(schedules) != 1 {
		t.Fatalf("schedules = %+v, %v", schedules, err)
	}
	protected, err := db.ListProtectedVMs(ctx)
	if err != nil || len(protected) != 1 || protected[0].VMName != newName {
		t.Fatalf("protected = %+v, %v", protected, err)
	}
	if err := db.ReleaseVMUsage(ctx, user.ID, host.ID, newName); err != nil {
		t.Fatal(err)
	}
	usage, err := db.UserUsage(ctx, user.ID)
	if err != nil || usage.VMs != 0 {
		t.Fatalf("usage after release = %+v, %v", usage, err)
	}
	drills, err := db.ListBackupDrills(ctx, host.ID, newName)
	if err != nil || len(drills) != 1 {
		t.Fatalf("drills = %+v, %v", drills, err)
	}
}
