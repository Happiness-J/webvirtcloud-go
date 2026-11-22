package web

import "testing"

func TestMemoryAmountUsesCurrentAllocation(t *testing.T) {
	if got := memoryAmount("8388608 KiB"); got != "8G" {
		t.Fatalf("memoryAmount() = %q", got)
	}
}

func TestMemoryAmountFallsBackWhenMissing(t *testing.T) {
	if got := memoryAmount(""); got != "4G" {
		t.Fatalf("memoryAmount() = %q", got)
	}
}
