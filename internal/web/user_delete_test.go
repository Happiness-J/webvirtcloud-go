package web

import "testing"

func TestUserDeleteAllowed(t *testing.T) {
	if !userDeleteAllowed("admin-session", "user-2", "viewer", false) {
		t.Fatal("ordinary user was rejected")
	}
	if userDeleteAllowed("user-1", "user-1", "viewer", false) {
		t.Fatal("current account was accepted")
	}
	if userDeleteAllowed("admin-session", "user-2", "admin", true) {
		t.Fatal("last enabled admin was accepted")
	}
}
