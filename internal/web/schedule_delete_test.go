package web

import (
	"os"
	"strings"
	"testing"
)

func TestScheduleDeleteRouteExists(t *testing.T) {
	data, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `POST /hosts/{id}/vms/{vm}/schedule/delete`) {
		t.Fatal("schedule delete route missing")
	}
}
