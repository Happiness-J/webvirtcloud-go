package web

import (
	"context"
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

type poolPathStub struct {
	paths map[string]string
}

func (s poolPathStub) PoolTargetPath(_ context.Context, _, pool string) (string, error) {
	return s.paths[pool], nil
}

func TestDefaultPoolPathOccupied(t *testing.T) {
	occupied, err := defaultPoolPathOccupied(context.Background(), poolPathStub{paths: map[string]string{"images": "/var/lib/libvirt/images"}}, "host", []libvirt.Pool{{Name: "images"}})
	if err != nil || !occupied {
		t.Fatalf("occupied=%v err=%v", occupied, err)
	}
	occupied, err = defaultPoolPathOccupied(context.Background(), poolPathStub{paths: map[string]string{"other": "/srv/images"}}, "host", []libvirt.Pool{{Name: "other"}})
	if err != nil || occupied {
		t.Fatalf("occupied=%v err=%v", occupied, err)
	}
}
