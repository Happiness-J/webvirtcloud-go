package web

import "testing"

func TestPromLabelEscapes(t *testing.T) {
	if got := promLabel("a\"b\\c"); got != "\"a\\\"b\\\\c\"" {
		t.Fatalf("label %s", got)
	}
}

func TestParseMetricNumber(t *testing.T) {
	if value, ok := parseMetricNumber("12.5 %"); !ok || value != 12.5 {
		t.Fatalf("%v %v", value, ok)
	}
	if _, ok := parseMetricNumber("n/a"); ok {
		t.Fatal("accepted text")
	}
}
