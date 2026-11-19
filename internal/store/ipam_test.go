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

func TestNextFreeAddressSkipsUsedAddress(t *testing.T) {
	prefix := netip.MustParsePrefix("192.0.2.0/30")
	first, ok := nextFreeAddress(prefix, nil)
	if !ok || first.String() != "192.0.2.1" {
		t.Fatalf("first = %s %v", first, ok)
	}
	second, ok := nextFreeAddress(prefix, map[string]bool{first.String(): true})
	if !ok || second.String() != "192.0.2.2" {
		t.Fatalf("second = %s %v", second, ok)
	}
	if _, ok := nextFreeAddress(prefix, map[string]bool{"192.0.2.1": true, "192.0.2.2": true}); ok {
		t.Fatal("full prefix still returned an address")
	}
}
