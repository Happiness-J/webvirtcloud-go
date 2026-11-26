package web

import (
	"time"

	"webvirtcloud-go/internal/store"
)

const tokenUnusedAfter = 30 * 24 * time.Hour

// staleTokenCount is how many of the current account's still-valid API tokens
// were last used more than thirty days ago, or were never used and are already
// older than thirty days. Revoked and expired tokens are left out. The count
// never revokes a token.
func staleTokenCount(tokens []store.APIToken, now time.Time) int {
	count := 0
	for _, token := range tokens {
		if !tokenActiveAt(token, now) {
			continue
		}
		seen := token.CreatedAt
		if token.LastUsedAt != nil {
			seen = *token.LastUsedAt
		}
		if !seen.IsZero() && now.Sub(seen) >= tokenUnusedAfter {
			count++
		}
	}
	return count
}

func tokenActiveAt(token store.APIToken, now time.Time) bool {
	if token.RevokedAt != nil {
		return false
	}
	if token.ExpiresAt != nil && !token.ExpiresAt.After(now) {
		return false
	}
	return true
}
