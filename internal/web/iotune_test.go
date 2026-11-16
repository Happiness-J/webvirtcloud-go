package web

import (
	"strings"
	"testing"
)

func TestPerformanceArgs(t *testing.T) {
	args, err := nicQueueArgs("web-01", "52:54:00:11:22:33", 4)
	if err != nil || strings.Join(args, " ") != "web-01 --edit mac=52:54:00:11:22:33 --network driver_queues=4" {
		t.Fatalf("queues=%#v err=%v", args, err)
	}
	args, err = diskTuneArgs("web-01", "vda", "1048576", "0", "1000", "500")
	if err != nil || !strings.Contains(strings.Join(args, " "), "--read-iops-sec 1000") {
		t.Fatalf("tune=%#v err=%v", args, err)
	}
	if _, err = diskTuneArgs("web-01", "vda", "-1", "0", "0", "0"); err == nil {
		t.Fatal("negative limit accepted")
	}
	args, err = diskDiscardArgs("web-01", "vda", "unmap")
	if err != nil || strings.Join(args, " ") != "web-01 --edit target=vda --disk discard=unmap" {
		t.Fatalf("discard=%#v err=%v", args, err)
	}
}

func TestParseNICChoices(t *testing.T) {
	output := "Interface Type Source Model MAC\n" +
		"---------------------------------------------\n" +
		"vnet0 network default virtio 52:54:00:11:22:33\n"
	got := parseNICChoices(output)
	if len(got) != 1 || got[0].MAC != "52:54:00:11:22:33" {
		t.Fatalf("nics=%#v", got)
	}
}
