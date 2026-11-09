package id

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewIdentifiers(t *testing.T) {
	withPrefix := New("host")
	if !strings.HasPrefix(withPrefix, "host_") {
		t.Fatalf("New(host) = %q, want host_ prefix", withPrefix)
	}
	raw := strings.TrimPrefix(withPrefix, "host_")
	if len(raw) != 32 {
		t.Fatalf("identifier payload length = %d, want 32", len(raw))
	}
	if _, err := hex.DecodeString(raw); err != nil {
		t.Fatalf("identifier payload is not hex: %v", err)
	}

	withoutPrefix := New("")
	if len(withoutPrefix) != 32 || strings.Contains(withoutPrefix, "_") {
		t.Fatalf("New(\"\") = %q, want 32 hex characters", withoutPrefix)
	}
	if withoutPrefix == New("") {
		t.Fatal("two empty-prefix identifiers unexpectedly matched")
	}
}
