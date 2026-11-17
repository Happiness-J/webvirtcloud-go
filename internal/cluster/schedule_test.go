package cluster

import "testing"

func hosts() []Host {
	return []Host{
		{ID: "a", Name: "alpha", URI: "qemu+ssh://alpha/system", Enabled: true, Reachable: true, CPUPercent: 80, MemPercent: 70, RunningVMs: 8},
		{ID: "b", Name: "beta", URI: "qemu+ssh://beta/system", Enabled: true, Reachable: true, CPUPercent: 20, MemPercent: 30, RunningVMs: 2},
		{ID: "c", Name: "gamma", URI: "qemu+tls://gamma/system", Enabled: true, Reachable: false, CPUPercent: 1, MemPercent: 1, RunningVMs: 0},
		{ID: "d", Name: "delta", URI: "qemu+ssh://delta/system", Enabled: true, Maintenance: true, Reachable: true, CPUPercent: 5, MemPercent: 5, RunningVMs: 0},
		{ID: "e", Name: "local", URI: "qemu:///system", Enabled: true, Reachable: true, CPUPercent: 1, MemPercent: 1, RunningVMs: 0},
	}
}

func TestBalancedSkipsUnusableHosts(t *testing.T) {
	decision, err := Place(hosts(), Request{VMName: "web", SourceHostID: "a", Policy: PolicyBalanced, RequireShared: true})
	if err != nil {
		t.Fatal(err)
	}
	if decision.HostID != "b" {
		t.Fatalf("got %s, want beta", decision.HostName)
	}
}

func TestSpreadPrefersFewerVMs(t *testing.T) {
	items := hosts()
	items = append(items, Host{ID: "f", Name: "idle", URI: "qemu+ssh://idle/system", Enabled: true, Reachable: true, CPUPercent: 40, MemPercent: 40, RunningVMs: 0})
	decision, err := Place(items, Request{SourceHostID: "a", Policy: PolicySpread})
	if err != nil {
		t.Fatal(err)
	}
	if decision.HostID != "f" {
		t.Fatalf("got %s", decision.HostName)
	}
}

func TestPackPrefersExistingLoad(t *testing.T) {
	decision, err := Place(hosts(), Request{SourceHostID: "missing", Policy: PolicyPack})
	if err != nil {
		t.Fatal(err)
	}
	if decision.HostID != "a" {
		t.Fatalf("got %s, want the busier host", decision.HostName)
	}
}

func TestNoCandidate(t *testing.T) {
	_, err := Place(hosts(), Request{SourceHostID: "b", Policy: "custom"})
	if err == nil {
		t.Fatal("invalid policy accepted")
	}
	_, err = Place([]Host{{ID: "a", Enabled: true, Reachable: true, URI: "qemu+ssh://a/system"}}, Request{SourceHostID: "a", Policy: PolicyBalanced})
	if err == nil {
		t.Fatal("source host accepted as its own target")
	}
}
