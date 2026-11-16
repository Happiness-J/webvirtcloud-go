package web

import "testing"

func TestBootOrderArg(t *testing.T) {
	got, err := bootOrderArg("network,hd", "on")
	if err != nil || got != "network,hd,menu=on" {
		t.Fatalf("boot=%q err=%v", got, err)
	}
	if _, err = bootOrderArg("network,network", "off"); err == nil {
		t.Fatal("duplicate order accepted")
	}
	args, err := bootEditArgs("web-01", "hd,cdrom,network", "off")
	if err != nil || len(args) != 4 || args[3] != "hd,cdrom,network,menu=off" {
		t.Fatalf("args=%#v err=%v", args, err)
	}
}
