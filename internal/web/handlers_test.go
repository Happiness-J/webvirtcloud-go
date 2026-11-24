package web

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func actionNames(actions []vmActionView) []string {
	names := make([]string, len(actions))
	for i, action := range actions {
		names[i] = action.Name
	}
	return names
}

func TestVMActionsFollowPowerState(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		state     string
		autostart string
		want      []string
	}{
		{
			name: "stopped admin", role: "admin", state: "shut off", autostart: "enable",
			want: []string{"start", "managed-start", "autostart-off"},
		},
		{
			name: "running operator", role: "operator", state: "running", autostart: "disable",
			want: []string{"shutdown", "reboot", "destroy", "suspend", "managed-save", "autostart-on"},
		},
		{
			name: "paused admin", role: "admin", state: "paused", autostart: "disable",
			want: []string{"resume", "destroy", "autostart-on"},
		},
		{name: "desktop stopped", role: "desktop", state: "shut off", autostart: "disable", want: []string{"start"}},
		{name: "desktop running", role: "desktop", state: "running", autostart: "enable", want: []string{"shutdown"}},
		{name: "viewer", role: "viewer", state: "running", autostart: "enable", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := actionNames(vmActions(tc.role, tc.state, tc.autostart))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("actions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConsoleAvailableOnlyForRunningStates(t *testing.T) {
	for _, state := range []string{"running", "paused", " Running "} {
		if !consoleAvailable(state) {
			t.Errorf("consoleAvailable(%q) = false", state)
		}
	}
	for _, state := range []string{"shut off", "crashed", "pmsuspended", ""} {
		if consoleAvailable(state) {
			t.Errorf("consoleAvailable(%q) = true", state)
		}
	}
}

func TestAuditDetailText(t *testing.T) {
	if got := auditDetailText(nil); got != "—" {
		t.Fatalf("empty detail = %q", got)
	}
	got := auditDetailText(map[string]any{
		"username":         "admin",
		"sessions_revoked": true,
	})
	if got != "sessions_revoked=true, username=admin" {
		t.Fatalf("detail = %q", got)
	}
}

func TestAuditActionName(t *testing.T) {
	if auditActionName("job.enqueue") != "提交任务" || auditActionName("auth.login") != "登录成功" || auditActionName("user.password_reset") != "重置登录密码" || auditActionName("custom.action") != "custom.action" {
		t.Fatal("unexpected audit action label")
	}
}

func TestHostsWithVMAccessKeepsAssignedHostsOnly(t *testing.T) {
	hosts := []store.Host{{ID: "host-a", Name: "a"}, {ID: "host-b", Name: "b"}, {ID: "host-c", Name: "c"}}
	got := hostsWithVMAccess(hosts, []store.VMAccess{{HostID: "host-c", VMName: "desk-1"}, {HostID: "missing", VMName: "gone"}})
	if len(got) != 1 || got[0].ID != "host-c" {
		t.Fatalf("hosts = %#v, want only host-c", got)
	}
	if got := hostsWithVMAccess(hosts, nil); len(got) != 0 {
		t.Fatalf("hosts without assignments = %#v, want none", got)
	}
}

func TestDomainsWithVMAccessKeepsAssignedVMsOnly(t *testing.T) {
	domains := []libvirt.Domain{{Name: "web"}, {Name: "desk"}, {Name: "db"}}
	access := []store.VMAccess{{HostID: "host-a", VMName: "desk"}, {HostID: "host-b", VMName: "web"}}
	got := domainsWithVMAccess("host-a", domains, access)
	if len(got) != 1 || got[0].Name != "desk" {
		t.Fatalf("domains = %#v, want only desk", got)
	}
	if got := domainsWithVMAccess("host-a", domains, nil); len(got) != 0 {
		t.Fatalf("domains without assignments = %#v, want none", got)
	}
}

func TestHostDetailRendersOutsideMaintenance(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "host", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host":     store.Host{ID: "host-1", Name: "compute", Enabled: true, CreatedAt: time.Unix(1, 0)},
		"Redacted": "qemu:///system", "Jobs": []store.Job{},
	}}
	var rendered bytes.Buffer
	if err := templates["host_detail"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	if !bytes.Contains(rendered.Bytes(), []byte("进入维护")) || bytes.Contains(rendered.Bytes(), []byte("进入维护</button>{{end}}")) {
		t.Fatalf("maintenance button was not rendered: %s", body)
	}
	if !bytes.Contains(rendered.Bytes(), []byte("</button>")) {
		t.Fatal("rendered host detail has no buttons")
	}
}

func TestHostGrantCoversRequestedHost(t *testing.T) {
	granted := []store.HostAccess{{HostID: "host-a"}, {HostID: "host-b"}}
	if !hostGrantCovers("host-b", granted) {
		t.Fatal("assigned host was rejected")
	}
	if hostGrantCovers("host-c", granted) {
		t.Fatal("unassigned host was accepted")
	}
	if !hostGrantCovers("host-c", nil) {
		t.Fatal("an account without host grants should keep the default visibility")
	}
}

func TestAccountSecurityShowsFullCreatedToken(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 43)
	page := pageData{Title: "security", CSRF: "csrf", User: &store.User{Role: "operator", Username: "ops"}, Data: map[string]any{
		"TOTPEnabled": false, "Pending": "", "Sessions": []store.SessionInfo{}, "Tokens": []store.APIToken{},
		"Roles": []string{"viewer"}, "CreatedToken": token,
	}}
	var rendered bytes.Buffer
	if err := templates["account_security"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered.Bytes(), []byte(token)) {
		t.Fatal("created token was truncated or omitted")
	}
}

func TestVMGrantCoversAssignedHostOnly(t *testing.T) {
	access := []store.VMAccess{{HostID: "host-a", VMName: "desk"}}
	if !vmGrantCovers("host-a", "desk", access) {
		t.Fatal("assigned VM was rejected")
	}
	if vmGrantCovers("host-a", "other", access) {
		t.Fatal("unassigned VM on an assigned host was accepted")
	}
	if !vmGrantCovers("host-b", "other", access) {
		t.Fatal("host without VM assignments was rejected")
	}
	if !vmGrantCovers("host-a", "other", nil) {
		t.Fatal("account without VM assignments was rejected")
	}
}

func TestNetworkPageShowsAddressPools(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "networks", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Host": store.Host{ID: "host-1", Name: "compute"}, "Networks": []libvirt.Network{}, "IPPools": []store.IPPool{}, "Allocations": map[string][]store.IPAllocation{},
	}}
	var rendered bytes.Buffer
	if err := templates["networks"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered.Bytes(), []byte("地址池")) || !bytes.Contains(rendered.Bytes(), []byte("创建地址池")) {
		t.Fatal("address pool section was not rendered")
	}
}

func TestVMDetailSnapshotTreeRenders(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "vm", CSRF: "csrf", User: &store.User{Role: "operator"}, Data: map[string]any{
		"Host": store.Host{ID: "host"}, "VM": "web", "Video": videoDevice{}, "MaxMemoryBytes": uint64(1024), "Balloon": balloonView{},
		"Detail": libvirt.DomainDetail{Domain: libvirt.Domain{Name: "web", State: "running"}, Info: map[string]string{}, Snapshots: []libvirt.Snapshot{{Name: "base", Kind: "internal", State: "shutoff"}, {Name: "child", Parent: "base", Kind: "external", Depth: 1, Current: true, State: "running"}}},
		"Boot":   bootInventory{Firmware: "UEFI", SecureBoot: "已启用", TPM: "tpm-crb 2.0", BootOrder: "硬盘", BootMenu: "关闭"},
	}}
	var buf bytes.Buffer
	if err := templates["vm_detail"].ExecuteTemplate(&buf, "base", page); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{"快照树", "内部", "外部", "父快照", "正在运行", "固件与引导", "UEFI", "已启用", "tpm-crb 2.0"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
