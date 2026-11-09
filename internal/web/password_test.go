package web

import (
	"strings"
	"testing"
)

func TestValidateNewPassword(t *testing.T) {
	valid := []string{
		"correct horse battery staple",
		"中文密码足够长十个字符",
	}
	for _, password := range valid {
		if err := validateNewPassword(password); err != nil {
			t.Errorf("validateNewPassword(%q) = %v", password, err)
		}
	}

	invalid := []struct {
		name     string
		password string
		want     string
	}{
		{name: "too short", password: "short", want: "至少"},
		{name: "too long", password: strings.Repeat("a", 4097), want: "过长"},
		{name: "invalid UTF-8", password: string([]byte{0xff, 0xfe, 0xfd}), want: "UTF-8"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNewPassword(tc.password)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateNewPassword() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
