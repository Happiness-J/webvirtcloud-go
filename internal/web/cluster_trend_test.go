package web

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestSummarizeCapacityKeepsOneDayShape(t *testing.T) {
	base := time.Now().Add(-2 * time.Hour)
	samples := []store.HostCapacitySample{
		{HostID: "h", CPUPercent: 10, MemPercent: 20, RunningVMs: 1, SampledAt: base},
		{HostID: "h", CPUPercent: 80, MemPercent: 40, RunningVMs: 6, SampledAt: base.Add(time.Hour)},
		{HostID: "h", CPUPercent: 30, MemPercent: 90, RunningVMs: 2, SampledAt: base.Add(2 * time.Hour)},
	}
	got := summarizeCapacity(samples)
	if got.Points != 3 || got.LatestCPU != 30 || got.PeakCPU != 80 || got.PeakMem != 90 || got.MinVMs != 1 || got.MaxVMs != 6 {
		t.Fatalf("summary = %#v", got)
	}
	for _, series := range []string{got.CPUPoints, got.MemPoints, got.VMPoints} {
		for _, point := range strings.Fields(series) {
			parts := strings.Split(point, ",")
			if len(parts) != 2 {
				t.Fatalf("point %q", point)
			}
			x, errX := strconv.Atoi(parts[0])
			y, errY := strconv.Atoi(parts[1])
			if errX != nil || errY != nil || x < 0 || x > 240 || y < 6 || y > 46 {
				t.Fatalf("point %q outside chart", point)
			}
		}
	}
}

func TestSummarizeCapacityEmpty(t *testing.T) {
	got := summarizeCapacity(nil)
	if got.Points != 0 || got.MinVMs != 0 || got.CPUPoints != "" {
		t.Fatalf("empty summary = %#v", got)
	}
}
