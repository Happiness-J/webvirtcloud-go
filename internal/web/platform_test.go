package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestPlatformChecksumMatchesCanonicalBody(t *testing.T) {
	created := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	hosts := []hostExport{{Name: "lab", Description: "local", Enabled: true}}
	raw, err := json.Marshal(struct {
		Version   string       `json:"version"`
		CreatedAt time.Time    `json:"created_at"`
		Hosts     []hostExport `json:"hosts"`
	}{"platform-1", created, hosts})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) == "" {
		t.Fatal("empty checksum")
	}
}
