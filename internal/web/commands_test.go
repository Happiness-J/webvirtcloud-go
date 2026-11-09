package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestJobHostLabel(t *testing.T) {
	tests := []struct {
		name string
		job  store.Job
		want string
	}{
		{name: "existing host", job: store.Job{HostID: "host-1", HostName: "kvm-1", Tool: "virsh"}, want: "kvm-1"},
		{name: "legacy host-bound tool without id", job: store.Job{Tool: "virt-install"}, want: "未绑定主机（不会执行）"},
		{name: "deleted host with stale id", job: store.Job{HostID: "host-1", Tool: "virsh"}, want: "主机已删除或不可用"},
		{name: "legacy unbound job", job: store.Job{Tool: "qemu-img"}, want: "未绑定主机（不会执行）"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jobHostLabel(tc.job); got != tc.want {
				t.Fatalf("jobHostLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommandArgsFromForm(t *testing.T) {
	cases := []struct {
		name  string
		form  url.Values
		want  []string
		error bool
	}{
		{
			name: "builder json", form: url.Values{"mode": {"builder"}, "args_json": {`["start","web-prod","--paused"]`}},
			want: []string{"start", "web-prod", "--paused"},
		},
		{
			name: "advanced raw args", form: url.Values{"mode": {"advanced"}, "raw_args": {`snapshot-list --domain "web prod"`}},
			want: []string{"snapshot-list", "--domain", "web prod"},
		},
		{
			name: "legacy raw args", form: url.Values{"args": {"list --all"}},
			want: []string{"list", "--all"},
		},
		{name: "empty builder for zero-argument tools", form: url.Values{"mode": {"builder"}, "args_json": {`[]`}}, want: []string{}},
		{name: "malformed builder", form: url.Values{"mode": {"builder"}, "args_json": {`{`}}, error: true},
		{name: "empty advanced", form: url.Values{"mode": {"advanced"}, "raw_args": {``}}, error: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/hosts/host/command", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			got, err := commandArgsFromForm(req)
			if tc.error {
				if err == nil {
					t.Fatalf("commandArgsFromForm() = %#v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("commandArgsFromForm() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
