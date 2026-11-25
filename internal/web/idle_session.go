package web

import (
	"time"

	"webvirtcloud-go/internal/store"
)

const sessionIdleAfter = 24 * time.Hour

// idleSessionCount is how many of the current account's other sessions have
// not been used for a day. It never revokes a session.
func idleSessionCount(sessions []store.SessionInfo, now time.Time) int {
	count := 0
	for _, session := range sessions {
		if session.Current || session.LastSeen.IsZero() {
			continue
		}
		if now.Sub(session.LastSeen) >= sessionIdleAfter {
			count++
		}
	}
	return count
}
