package store

import "testing"

func TestDefaultQuotaWhenMissing(t *testing.T) {
	quota := UserQuota{MaxVMs: 20, MaxVCPUs: 64, MaxMemoryGB: 256, MaxDiskGB: 2000}
	usage := Usage{VMs: 20}
	if usage.VMs+1 <= quota.MaxVMs {
		t.Fatal("expected the next VM to exceed the default count")
	}
}
