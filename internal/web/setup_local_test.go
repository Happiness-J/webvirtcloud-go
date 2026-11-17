package web

import (
	"testing"

	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

func TestLocalSetupHostSkipsRemote(t *testing.T) {
	cipher, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := cipher.SealFor("host:remote", "qemu+ssh://remote/system")
	if err != nil {
		t.Fatal(err)
	}
	local, err := cipher.SealFor("host:local", "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	host, ok := localSetupHost([]store.Host{
		{ID: "remote", Enabled: true, URICiphertext: remote},
		{ID: "local", Enabled: true, URICiphertext: local},
	}, cipher)
	if !ok || host.ID != "local" {
		t.Fatalf("got %#v %v", host, ok)
	}
}
