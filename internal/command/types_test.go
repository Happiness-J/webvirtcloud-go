package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResultOutputMarksTruncatedOutput(t *testing.T) {
	result := Result{Stdout: "partial stdout", Stderr: "partial stderr", Truncated: true}
	output := result.Output()
	if !strings.Contains(output, "partial stdout\npartial stderr\n") {
		t.Fatalf("output does not contain command streams: %q", output)
	}
	if !strings.Contains(output, "输出已截断") || !strings.Contains(output, "WVC_MAX_OUTPUT_BYTES") {
		t.Fatalf("output does not explain truncation: %q", output)
	}
}

func TestResultOutputLeavesCompleteOutputUnchanged(t *testing.T) {
	result := Result{Stdout: "complete"}
	if output := result.Output(); output != "complete" {
		t.Fatalf("Output() = %q, want complete", output)
	}
}

func TestRunnerAddsOnlyOneTruncationMarker(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "virt-what")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%0500d' 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{
		paths:          map[string]string{"virt-what": script},
		maxOutputBytes: 16,
		maxArgs:        16,
		maxStdinBytes:  64,
		defaultTimeout: time.Second,
		maxTimeout:     time.Second,
	}
	result, err := runner.Run(context.Background(), Request{Tool: "virt-what"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatal("result was not marked truncated")
	}
	output := result.Output()
	if strings.Contains(output, "[output truncated]") {
		t.Fatalf("runner leaked the English truncation marker: %q", output)
	}
	if got := strings.Count(output, "[输出已截断"); got != 1 {
		t.Fatalf("truncation marker count = %d, output = %q", got, output)
	}
}
