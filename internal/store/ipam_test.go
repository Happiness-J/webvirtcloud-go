package store

import (
	"net/netip"
	"testing"
)

func TestPrefixContainsUsableAddress(t *testing.T) {
	prefix := netip.MustParsePrefix("192.0.2.0/30")
	first := prefix.Addr().Next()
	if !prefix.Contains(first) || first.String() == prefix.Addr().String() {
		t.Fatalf("first usable %s", first)
	}
}
