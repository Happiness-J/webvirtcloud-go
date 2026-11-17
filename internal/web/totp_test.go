package web

import (
	"testing"
	"time"
)

func TestTOTPRoundTripAndWindow(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	code, ok := totpAt(secret, now)
	if !ok || len(code) != 6 {
		t.Fatalf("code %q ok=%v", code, ok)
	}
	if !validTOTP(secret, code, now.Add(20*time.Second)) {
		t.Fatal("expected current window")
	}
	if !validTOTP(secret, code, now.Add(30*time.Second)) {
		t.Fatal("expected adjacent window")
	}
	if validTOTP(secret, code, now.Add(90*time.Second)) {
		t.Fatal("expected distant code to fail")
	}
	if validTOTP(secret, "12ab56", now) || validTOTP("", code, now) {
		t.Fatal("expected malformed input to fail")
	}
}

func TestTokenRoleCannotEscalate(t *testing.T) {
	if allowedTokenRole("viewer", "admin") || allowedTokenRole("operator", "admin") {
		t.Fatal("token role escalated")
	}
	if !allowedTokenRole("operator", "viewer") || !allowedTokenRole("desktop", "desktop") {
		t.Fatal("expected permitted token role")
	}
}
