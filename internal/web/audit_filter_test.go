package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuditDateFilter(t *testing.T) {
	request := httptest.NewRequest("GET", "/audit?since=2026-01-02&until=2026-01-03&host=host&vm=web", nil)
	server := &Server{}
	filter, ok := server.auditFilter(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("filter rejected")
	}
	if filter.HostID != "host" || filter.VMName != "web" {
		t.Fatalf("identity = %#v", filter)
	}
	if filter.Since.Format("2006-01-02") != "2026-01-02" || !filter.Until.Equal(filter.Since.Add(48*time.Hour)) {
		t.Fatalf("range = %s %s", filter.Since, filter.Until)
	}
	url := auditURL("/audit/export", filter, 0)
	for _, part := range []string{"since=2026-01-02", "until=2026-01-03", "host=host", "vm=web"} {
		if !strings.Contains(url, part) {
			t.Fatalf("url %s missing %s", url, part)
		}
	}
}
