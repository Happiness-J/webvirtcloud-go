package store

import (
	"context"
	"testing"
)

func TestClusterPolicyValidation(t *testing.T) {
	db := &Store{}
	if err := db.SaveClusterPolicy(context.TODO(), ClusterPolicy{Policy: "random", FailAfter: 3}); err == nil {
		t.Fatal("unknown policy accepted")
	}
	if err := db.SaveClusterPolicy(context.TODO(), ClusterPolicy{Policy: "balanced", FailAfter: 1}); err == nil {
		t.Fatal("fail-after below two accepted")
	}
}

func TestHostHealthRange(t *testing.T) {
	db := &Store{}
	if err := db.RecordHostHealth(context.TODO(), HostHealth{CPUPercent: 120}); err == nil {
		t.Fatal("cpu over 100 accepted")
	}
}
