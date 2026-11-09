package id

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a random, URL-safe identifier. The prefix is for human
// readability and should not be treated as a security boundary.
func New(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	if prefix == "" {
		return hex.EncodeToString(b)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
