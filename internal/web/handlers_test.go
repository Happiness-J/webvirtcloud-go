package web

import (
	"slices"
	"testing"
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
			want: []string{"start", "autostart-off"},
		},
		{
			name: "running operator", role: "operator", state: "running", autostart: "disable",
			want: []string{"shutdown", "reboot", "destroy", "suspend", "autostart-on"},
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
	if auditActionName("job.enqueue") != "提交任务" || auditActionName("custom.action") != "custom.action" {
		t.Fatal("unexpected audit action label")
	}
}
