package web

import (
	"os"
	"strings"
	"testing"
)

func TestVolumeConvertQueuesPoolRefresh(t *testing.T) {
	data, err := os.ReadFile("operations.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	convert := strings.Index(body, `case "volume-convert"`)
	refresh := strings.LastIndex(body, `[]string{"pool-refresh", pool}`)
	if convert < 0 || refresh < convert {
		t.Fatal("volume conversion does not queue a pool refresh")
	}
}
