package web

import (
	"strings"
	"testing"

	"webvirtcloud-go/internal/libvirt"
)

func TestConsoleSecurityWarnings(t *testing.T) {
	tests := []struct {
		name       string
		console    libvirt.Console
		want       int
		wantPhrase string
	}{
		{
			name:    "loopback with password",
			console: libvirt.Console{Address: "127.0.0.1", Password: "secret"},
		},
		{
			name:       "wildcard without password",
			console:    libvirt.Console{Address: "0.0.0.0"},
			want:       2,
			wantPhrase: "所有网络接口",
		},
		{
			name:       "LAN address",
			console:    libvirt.Console{Address: "10.20.30.40", Password: "secret"},
			want:       1,
			wantPhrase: "10.20.30.40",
		},
		{
			name:       "loopback without password",
			console:    libvirt.Console{Address: "::1"},
			want:       1,
			wantPhrase: "未设置密码",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warnings := consoleSecurityWarnings(tc.console)
			if len(warnings) != tc.want {
				t.Fatalf("warnings = %v, want %d", warnings, tc.want)
			}
			if tc.wantPhrase != "" && !strings.Contains(strings.Join(warnings, "\n"), tc.wantPhrase) {
				t.Fatalf("warnings = %v, want phrase %q", warnings, tc.wantPhrase)
			}
		})
	}
}
