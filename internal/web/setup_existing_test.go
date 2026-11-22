package web

import (
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestSetupSkipsExistingDefaultResources(t *testing.T) {
	pools := []libvirt.Pool{{Name: "default"}}
	networks := []libvirt.Network{{Name: "default"}}
	if !namedResourceExists(pools, "default") || !namedNetworkExists(networks, "default") {
		t.Fatal("existing defaults were not detected")
	}
	if namedResourceExists(nil, "default") || namedNetworkExists(nil, "default") {
		t.Fatal("missing defaults were treated as existing")
	}
}
