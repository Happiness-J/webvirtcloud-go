package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestClassifyFailureUsesStoredTextOnly(t *testing.T) {
	cases := map[string]string{
		"虚拟机名称无效":                            "name",
		"storage pool not found":             "notfound",
		"no space left on device":            "storage",
		"解密主机连接信息失败，详情见服务端日志":                "connect",
		"permission denied":                  "permission",
		"context deadline exceeded: timeout": "timeout",
		"domain already exists":              "conflict",
		"something unexpected":               "other",
		"":                                   "other",
	}
	for text, want := range cases {
		if got := classifyFailure(text); got != want {
			t.Fatalf("%q -> %s, want %s", text, got, want)
		}
	}
}

func TestFailureGroupsCountAndClip(t *testing.T) {
	long := strings.Repeat("名称无效 ", 40)
	jobs := []store.Job{
		{ID: "a", Status: "failed", Label: "创建", Error: "虚拟机名称无效"},
		{ID: "b", Status: "failed", Label: "创建", Error: long},
		{ID: "c", Status: "succeeded", Label: "开机", Error: ""},
		{ID: "d", Status: "failed", Label: "隐藏", Error: ""},
	}
	groups := failureGroups(jobs)
	if len(groups) != 2 || groups[0].Key != "name" || groups[0].Count != 2 || len(groups[0].Examples) != 2 {
		t.Fatalf("groups = %#v", groups)
	}
	if got := []rune(groups[0].Examples[1].Error); len(got) != 161 {
		t.Fatalf("clipped length %d", len(got))
	}
	if groups[1].Key != "other" || groups[1].Examples[0].Error != "失败原因已隐藏或未记录" {
		t.Fatalf("hidden group = %#v", groups[1])
	}
}
