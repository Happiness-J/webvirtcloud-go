package web

import (
	"strings"
	"testing"
)

func TestQMPArgs(t *testing.T) {
	args, err := qmpArgs("web-01", "status")
	if err != nil || strings.Join(args, " ") != `qemu-monitor-command web-01 --pretty {"execute":"query-status"}` {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	if _, err = qmpArgs("web-01", "quit"); err == nil {
		t.Fatal("arbitrary QMP command accepted")
	}
}
