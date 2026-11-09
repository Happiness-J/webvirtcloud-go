package console

import (
	"testing"
	"time"
)

func TestTokenRoundTripAndExpiry(t *testing.T) {
	tokens, err := NewTokens(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token, err := tokens.Issue("host", "vm", "user", ScopeWebSocket, now)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{HostID: "host", VM: "vm", UserID: "user"}
	claims, err := tokens.Validate(token, ScopeWebSocket, binding, now)
	if err != nil || claims.VM != "vm" {
		t.Fatalf("validate: %#v %v", claims, err)
	}
	if _, err := tokens.Validate(token, ScopeWebSocket, binding, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expected expired token")
	}
	if _, err := tokens.Validate(token+"x", ScopeWebSocket, binding, now); err == nil {
		t.Fatal("expected tampered token rejection")
	}
	if _, err := tokens.Validate(token, ScopePassword, binding, now); err == nil {
		t.Fatal("expected scope mismatch rejection")
	}
	wrongBinding := Binding{HostID: "host", VM: "other", UserID: "user"}
	if _, err := tokens.Validate(token, ScopeWebSocket, wrongBinding, now); err == nil {
		t.Fatal("expected binding mismatch rejection")
	}
	second, err := tokens.Issue("host", "vm", "user", ScopeWebSocket, now)
	if err != nil {
		t.Fatal(err)
	}
	if second == token {
		t.Fatal("tokens issued at the same instant must have distinct nonces")
	}
}
