package web

import (
	"os"
	"strings"
	"testing"
)

func TestClusterProtectedListHasUnprotectAction(t *testing.T) {
	data, err := os.ReadFile("templates/cluster.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `data-confirm="确定取消这台虚拟机的高可用保护吗？"`) {
		t.Fatal("unprotect confirmation missing")
	}
	if strings.Contains(body, `name="protected" value="1" checked`) && strings.Count(body, `name="protected"`) != 1 {
		t.Fatal("unprotect row submits protection unexpectedly")
	}
}
