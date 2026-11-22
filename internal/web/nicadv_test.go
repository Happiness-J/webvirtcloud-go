package web

import (
	"strings"
	"testing"
)

func TestNICAdvancedArgs(t *testing.T) {
	args, err := nicAdvancedArgs("web-01", "52:54:00:11:22:33", "10", "access", "1500", "", "up")
	if err != nil || !strings.Contains(strings.Join(args, " "), "access=10,mtu.size=1500,link=up") {
		t.Fatalf("args=%#v err=%v", args, err)
	}
	if _, err = nicAdvancedArgs("web-01", "52:54:00:11:22:33", "", "", "", "", ""); err == nil {
		t.Fatal("empty change accepted")
	}
}
