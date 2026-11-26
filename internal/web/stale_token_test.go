package web

import (
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestStaleTokenCountKeepsOnlyOldLiveTokens(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	revoked := old
	expired := now.Add(-time.Hour)
	tokens := []store.APIToken{
		{Name: "unused-old", CreatedAt: old},
		{Name: "used-old", CreatedAt: old, LastUsedAt: &old},
		{Name: "used-recently", CreatedAt: old, LastUsedAt: &recent},
		{Name: "new", CreatedAt: recent},
		{Name: "revoked", CreatedAt: old, RevokedAt: &revoked},
		{Name: "expired", CreatedAt: old, ExpiresAt: &expired},
	}
	if got := staleTokenCount(tokens, now); got != 2 {
		t.Fatalf("count = %d", got)
	}
}

func TestAccountPageRendersStaleTokens(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "account", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"TOTPEnabled": false, "Pending": false, "Sessions": []store.SessionInfo{}, "Tokens": []store.APIToken{},
		"Roles": []string{"viewer"}, "StaleTokens": 3,
	}}
	var rendered strings.Builder
	if err := templates["account_security"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"很久没用的 API 令牌", "3", "不吊销"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
