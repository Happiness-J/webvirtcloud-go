package web

import "testing"

func TestCPUTopologyArg(t *testing.T) {
	got, err := cpuTopologyArg(8, 2, 2, 2)
	if err != nil || got != "8,sockets=2,cores=2,threads=2" {
		t.Fatalf("topology = %q err=%v", got, err)
	}
	if _, err := cpuTopologyArg(8, 2, 2, 1); err == nil {
		t.Fatal("inconsistent topology accepted")
	}
}
