package web

import (
	"os"
	"strings"
	"testing"
)

func TestIPPoolDeleteRouteExists(t *testing.T) {
	data, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `POST /hosts/{id}/ip-pools/delete`) {
		t.Fatal("ip pool delete route missing")
	}
}
