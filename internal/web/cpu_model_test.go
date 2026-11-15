package web

import "testing"

func TestCPUModelArg(t *testing.T) {
	got, err := cpuModelArg("host-passthrough", "", true)
	if err != nil || got != "host-passthrough,+vmx" {
		t.Fatalf("cpu = %q err=%v", got, err)
	}
	if _, err := cpuModelArg("custom", "bad model", false); err == nil {
		t.Fatal("unsafe custom model accepted")
	}
}
