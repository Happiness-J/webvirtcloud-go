package web

import (
	"strings"
	"testing"
)

func TestDiskController(t *testing.T) {
	value, err := diskBusArg("nvme", 4)
	if err != nil || value != "nvme,queues=4" {
		t.Fatalf("bus=%q err=%v", value, err)
	}
	args, err := diskControllerArgs("web-01", "vda", "nvme", 4)
	if err != nil || strings.Join(args, " ") != "web-01 --edit target=vda --disk bus=nvme,queues=4" {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	got := parseDiskControllers(`<domain><devices><disk><target dev="vda" bus="nvme"/><driver queues="4"/></disk></devices></domain>`)
	if len(got) != 1 || got[0].Bus != "nvme" || got[0].Queues != 4 {
		t.Fatalf("controllers=%#v", got)
	}
}
