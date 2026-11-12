package jobs

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"webvirtcloud-go/internal/store"
)

func TestAuthorizeQueuedJob(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		tool    string
		args    []string
		role    string
		wantErr string
	}{
		{name: "command admin", kind: "command", tool: "virsh", args: []string{"list"}, role: "admin"},
		{name: "command operator", kind: "command", tool: "virsh", args: []string{"list"}, role: "operator", wantErr: "当前角色"},
		{name: "command viewer", kind: "command", tool: "qemu-img", args: []string{"info", "a"}, role: "viewer", wantErr: "当前角色"},
		{name: "vm action operator", kind: "vm-action", tool: "virsh", args: []string{"destroy", "vm"}, role: "operator"},
		{name: "vm action undefine admin", kind: "vm-action", tool: "virsh", args: []string{"undefine", "vm"}, role: "admin"},
		{name: "vm action undefine operator", kind: "vm-action", tool: "virsh", args: []string{"undefine", "vm"}, role: "operator", wantErr: "当前角色"},
		{name: "vm action viewer", kind: "vm-action", tool: "virsh", args: []string{"start", "vm"}, role: "viewer", wantErr: "当前角色"},
		{name: "vm action wrong tool", kind: "vm-action", tool: "qemu-img", args: []string{"info", "a"}, role: "admin", wantErr: "仅支持 virsh"},
		{name: "host test operator", kind: "host-test", tool: "virsh", args: []string{"version"}, role: "operator"},
		{name: "host test wrong tool", kind: "host-test", tool: "qemu-img", args: []string{"info", "a"}, role: "admin", wantErr: "不支持工具"},
		{name: "unknown kind", kind: "unknown", tool: "virsh", args: []string{"list"}, role: "admin", wantErr: "未知任务类型"},
		{name: "missing args", kind: "vm-action", tool: "virsh", args: nil, role: "admin", wantErr: "缺少命令参数"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := authorizeQueuedJob(tc.kind, tc.tool, tc.args, tc.role)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("authorizeQueuedJob() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("authorizeQueuedJob() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateJobPrincipal(t *testing.T) {
	tests := []struct {
		name      string
		user      store.User
		kind      string
		tool      string
		args      []string
		sensitive bool
		wantErr   string
	}{
		{
			name: "disabled API operator remains a job principal",
			user: store.User{ID: "system:api-operator", Role: "operator", Disabled: true},
			kind: "vm-action", tool: "virsh", args: []string{"start", "vm"},
		},
		{
			name: "disabled API admin remains a job principal",
			user: store.User{ID: "system:api-admin", Role: "admin", Disabled: true},
			kind: "vm-action", tool: "virsh", args: []string{"undefine", "vm"},
		},
		{
			name: "disabled regular user is rejected",
			user: store.User{ID: "user-1", Role: "operator", Disabled: true},
			kind: "vm-action", tool: "virsh", args: []string{"start", "vm"}, wantErr: "已被禁用",
		},
		{
			name: "API operator cannot run admin-only operation",
			user: store.User{ID: "system:api-operator", Role: "operator", Disabled: true},
			kind: "vm-action", tool: "virsh", args: []string{"undefine", "vm"}, wantErr: "当前角色",
		},
		{
			name: "API operator cannot run sensitive command",
			user: store.User{ID: "system:api-operator", Role: "operator", Disabled: true},
			kind: "host-test", tool: "virsh", args: []string{"version"}, sensitive: true, wantErr: "敏感命令",
		},
		{
			name: "API admin can run sensitive command",
			user: store.User{ID: "system:api-admin", Role: "admin", Disabled: true},
			kind: "command", tool: "virsh", args: []string{"list"}, sensitive: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJobPrincipal(tc.user, tc.kind, tc.tool, tc.args, tc.sensitive)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateJobPrincipal() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateJobPrincipal() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestRecoverJobPanic(t *testing.T) {
	w := &Worker{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	status, output, errText := func() (status, output, errText string) {
		defer w.recoverJobPanic(&store.Job{ID: "job-1", Tool: "virsh"}, &status, &output, &errText)
		panic("boom")
	}()
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	if output != "" {
		t.Fatalf("output = %q, want empty", output)
	}
	if !strings.Contains(errText, "内部错误") {
		t.Fatalf("errText = %q, want internal error", errText)
	}
}

func TestHeartbeatFailureLimit(t *testing.T) {
	if jobHeartbeatFailureLimit < 2 {
		t.Fatalf("heartbeat failure limit = %d, want at least 2", jobHeartbeatFailureLimit)
	}
	if jobHeartbeatFailureLimit*jobHeartbeatInterval > 5*time.Minute {
		t.Fatalf("heartbeat fail-closed delay = %s, want below the five-minute stale timeout",
			jobHeartbeatFailureLimit*jobHeartbeatInterval)
	}
}

func TestRunJobDoesNotExposeCorruptArgumentDetails(t *testing.T) {
	w := &Worker{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	status, output, errText := w.runJob(context.Background(), &store.Job{
		ID: "job-corrupt", Tool: "virsh", ArgsJSON: `{"unexpected":"shape"}`,
	})
	if status != "failed" || output != "" {
		t.Fatalf("status=%q output=%q", status, output)
	}
	if strings.Contains(errText, "unexpected") || strings.Contains(errText, "shape") {
		t.Fatalf("corrupt argument details leaked into job error: %q", errText)
	}
	if !strings.Contains(errText, "详情见服务端日志") {
		t.Fatalf("job error = %q, want safe diagnostic message", errText)
	}
}

func TestRunJobHidesCommandFailureDetails(t *testing.T) {
	message := "命令执行失败，请查看任务输出"
	if strings.Contains(message, "qemu") || strings.Contains(message, "secret") {
		t.Fatal("failure message contains backend details")
	}
}
