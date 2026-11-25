package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestIdleSessionCountSkipsCurrent(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	sessions := []store.SessionInfo{
		{ID: "old", LastSeen: now.Add(-48 * time.Hour)},
		{ID: "fresh", LastSeen: now.Add(-2 * time.Hour)},
		{ID: "mine", Current: true, LastSeen: now.Add(-72 * time.Hour)},
	}
	if got := idleSessionCount(sessions, now); got != 1 {
		t.Fatalf("count = %d", got)
	}
}

func TestAccountPageRendersIdleSessions(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "account", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"TOTPEnabled": false, "Pending": false, "Sessions": []store.SessionInfo{}, "Tokens": []store.APIToken{},
		"Roles": []string{"viewer"}, "IdleSessions": 2,
	}}
	var rendered strings.Builder
	if err := templates["account_security"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"超过一天未活动", "2", "不会强制下线"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
