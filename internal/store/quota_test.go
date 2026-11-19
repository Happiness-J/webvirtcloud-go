package store

import "testing"

func TestQuotaExceededAtEachBoundary(t *testing.T) {
	quota := UserQuota{MaxVMs: 2, MaxVCPUs: 4, MaxMemoryGB: 8, MaxDiskGB: 20}
	if quotaExceeded(Usage{VMs: 1, VCPUs: 2, MemoryGB: 4, DiskGB: 10}, quota, 2, 4, 10) {
		t.Fatal("request exactly at the remaining quota was rejected")
	}
	cases := []Usage{{VMs: 2}, {VCPUs: 3}, {MemoryGB: 5}, {DiskGB: 11}}
	for _, usage := range cases {
		if !quotaExceeded(usage, quota, 2, 4, 10) {
			t.Fatalf("usage %+v stayed within quota", usage)
		}
	}
}
