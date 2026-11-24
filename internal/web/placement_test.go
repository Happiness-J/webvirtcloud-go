package web

import "testing"

func TestParseHostTopology(t *testing.T) {
	xmlText := `<capabilities><host><topology><cells num="2"><cell id="0"><cpus num="2"><cpu id="0"/><cpu id="1"/></cpus></cell><cell id="1"><cpus num="2"><cpu id="2"/><cpu id="3"/></cpus></cell></cells></topology></host></capabilities>`
	got := parseHostTopology(xmlText)
	if got.Nodes != "0, 1" || got.CPUs != "0-3" {
		t.Fatalf("topology = %#v", got)
	}
	if got := parseHostTopology("<capabilities></capabilities>"); got.Nodes != "未读取到拓扑" {
		t.Fatalf("empty topology = %#v", got)
	}
}
