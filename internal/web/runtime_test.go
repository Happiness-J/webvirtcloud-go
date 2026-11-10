package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/store"
)

func TestLoadVMGroupsBoundsConcurrency(t *testing.T) {
	hosts := make([]store.Host, 64)
	for i := range hosts {
		hosts[i] = store.Host{ID: fmt.Sprintf("host-%d", i), Name: fmt.Sprintf("host-%d", i), Enabled: true}
	}
	var active, maximum atomic.Int64
	list := func(context.Context, string) ([]libvirt.Domain, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		return []libvirt.Domain{{Name: "vm"}}, nil
	}
	groups := loadVMGroups(context.Background(), hosts, 4, list)
	if len(groups) != len(hosts) {
		t.Fatalf("groups = %d, want %d", len(groups), len(hosts))
	}
	if got := maximum.Load(); got > 4 {
		t.Fatalf("maximum concurrency = %d, want <= 4", got)
	}
	for i, group := range groups {
		if group.Err != "" || len(group.Domains) != 1 || group.Domains[0].HostID != hosts[i].ID {
			t.Fatalf("group %d = %+v", i, group)
		}
	}
}

func TestLoadVMGroupsStopsStartingWorkAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hosts := make([]store.Host, 32)
	for i := range hosts {
		hosts[i] = store.Host{ID: fmt.Sprintf("host-%d", i), Enabled: true}
	}
	groups := loadVMGroups(ctx, hosts, 4, func(context.Context, string) ([]libvirt.Domain, error) {
		t.Fatal("listDomains called after cancellation")
		return nil, nil
	})
	for i, group := range groups {
		if group.Err != "请求已取消" {
			t.Fatalf("group %d error = %q", i, group.Err)
		}
	}
}

func TestLoadVMGroupsDoesNotExposeBackendErrors(t *testing.T) {
	hosts := []store.Host{{ID: "host-1", Name: "kvm-1", Enabled: true}}
	groups := loadVMGroups(context.Background(), hosts, 1, func(context.Context, string) ([]libvirt.Domain, error) {
		return nil, errors.New("ssh: /secret/key: permission denied")
	})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if groups[0].cause == nil {
		t.Fatal("underlying error was not retained for server-side logging")
	}
	if strings.Contains(groups[0].Err, "secret") || strings.Contains(groups[0].Err, "permission") {
		t.Fatalf("backend error leaked to the page: %q", groups[0].Err)
	}
}

func TestLoadVMGroupsIsolatesHostPanics(t *testing.T) {
	hosts := []store.Host{
		{ID: "panic-host", Name: "panic-host", Enabled: true},
		{ID: "healthy-host", Name: "healthy-host", Enabled: true},
	}
	groups := loadVMGroups(context.Background(), hosts, 2, func(_ context.Context, hostID string) ([]libvirt.Domain, error) {
		if hostID == "panic-host" {
			panic("simulated libvirt adapter panic")
		}
		return []libvirt.Domain{{Name: "vm"}}, nil
	})
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Err != "读取虚拟机清单时发生内部错误" || len(groups[0].Domains) != 0 {
		t.Fatalf("panicking host group = %+v", groups[0])
	}
	if groups[1].Err != "" || len(groups[1].Domains) != 1 {
		t.Fatalf("healthy host group = %+v", groups[1])
	}
}

func TestConsoleSlotIsNonBlocking(t *testing.T) {
	server := &Server{consoleSlots: make(chan struct{}, 1)}
	if !server.acquireConsoleSlot() {
		t.Fatal("first console slot was rejected")
	}
	if server.acquireConsoleSlot() {
		t.Fatal("console slot limit was not enforced")
	}
	server.releaseConsoleSlot()
	if !server.acquireConsoleSlot() {
		t.Fatal("released console slot was not reusable")
	}
}

func TestSessionTouchSchedulingIsBounded(t *testing.T) {
	server := &Server{touchSlots: make(chan struct{}, 1)}
	server.touchSlots <- struct{}{}
	if server.touchSessionAsync("token", "127.0.0.1", "test") {
		t.Fatal("session touch was scheduled beyond the worker limit")
	}
}

func TestRenderStatusDoesNotCommitPartialPage(t *testing.T) {
	bad := template.Must(template.New("error").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("render failure") },
	}).Parse(`{{define "base"}}partial page{{fail}}{{end}}`))
	server := &Server{
		tmpl: map[string]*template.Template{"error": bad},
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	recorder := httptest.NewRecorder()
	server.renderStatus(recorder, httptest.NewRequest(http.MethodGet, "http://panel.example/", nil), http.StatusOK, "error", pageData{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "partial page") {
		t.Fatal("partial template output was committed")
	}
}

func TestRecovererDoesNotAppendToCommittedResponse(t *testing.T) {
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := server.recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial page"))
		panic("after commit")
	}))
	recorder := httptest.NewRecorder()
	status := &statusWriter{ResponseWriter: recorder, status: http.StatusOK}
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "http://panel.example/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Body.String(); got != "partial page" {
		t.Fatalf("body = %q, want the already committed content only", got)
	}
}

func TestRecovererWrites500BeforeCommit(t *testing.T) {
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := server.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("before commit")
	}))
	recorder := httptest.NewRecorder()
	status := &statusWriter{ResponseWriter: recorder, status: http.StatusOK}
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "http://panel.example/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "internal server error") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestStatusWriterFlushCommitsResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	status := &statusWriter{ResponseWriter: recorder, status: http.StatusOK}
	status.Flush()
	if !status.Committed() {
		t.Fatal("flushed response was not marked committed")
	}
	handler := (&Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}).recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("after flush")
	}))
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "http://panel.example/", nil))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "internal server error") {
		t.Fatalf("flushed response was modified: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestDockerIgnoreExcludesSSHMaterial(t *testing.T) {
	data, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	patterns := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns[line] = true
		}
	}
	for _, want := range []string{"deploy/ssh", "**/*.pem", "**/id_ed25519", "**/id_rsa"} {
		if !patterns[want] {
			t.Errorf(".dockerignore is missing %q", want)
		}
	}
}

func TestSystemdUnitHardensService(t *testing.T) {
	data, err := os.ReadFile("../../deploy/wvc.service")
	if err != nil {
		t.Fatal(err)
	}
	unit := string(data)
	for _, want := range []string{
		"NoNewPrivileges=true",
		"CapabilityBoundingSet=\n",
		"AmbientCapabilities=\n",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"ProtectKernelLogs=true",
		"ProtectClock=true",
		"RestrictRealtime=true",
		"RestrictNamespaces=~user",
		"SystemCallArchitectures=native",
		"LimitCORE=0",
		"TasksMax=1024",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("service unit is missing %q", want)
		}
	}
}

func TestInstallerDoesNotPrintBootstrapPassword(t *testing.T) {
	data, err := os.ReadFile("../../deploy/install-ubuntu-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if strings.Contains(script, `cat "${initial_password_file}"`) {
		t.Fatal("installer prints the generated bootstrap password into installer logs")
	}
	if !strings.Contains(script, "intentionally not printed") {
		t.Fatal("installer does not document why the bootstrap password is withheld")
	}
	if !strings.Contains(script, `GRANT_IMAGE_ACCESS=${WVC_GRANT_IMAGE_ACCESS:-0}`) ||
		!strings.Contains(script, `setfacl -R -m "u:${SERVICE_USER}:rwX" "${IMAGE_DIR}"`) {
		t.Fatal("installer is missing reviewed image directory ACL support")
	}
	if !strings.Contains(script, "systemctl restart webvirtcloud-go") {
		t.Fatal("installer can install a new binary without restarting the running service")
	}
	if !strings.Contains(script, `"${PROJECT_DIR}/deploy/60-webvirtcloud-go.rules"`) ||
		!strings.Contains(script, "/etc/polkit-1/rules.d/60-webvirtcloud-go.rules") {
		t.Fatal("installer does not install the scoped libvirt secure-domain policy")
	}
}

func TestStaticHandlerRejectsLegacyNoVNCAssets(t *testing.T) {
	handler := (&Server{}).staticHandler()
	for _, path := range []string{
		"/novnc/app/ui.js",
		"/novnc/vendor/browser-es-module-loader/dist/browser-es-module-loader.js",
		"/novnc/vendor/promise.js",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://panel.example"+path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://panel.example/novnc/core/rfb.js", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("noVNC core status = %d, want 200", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://panel.example/command.js", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "command-center") {
		t.Fatalf("command builder status = %d", recorder.Code)
	}
}

func TestLoginKeysIncludeIPAndGlobalDimensions(t *testing.T) {
	keys := loginKeys("203.0.113.4", " Alice ")
	if len(keys) != 3 {
		t.Fatalf("login keys = %v", keys)
	}
	for i, prefix := range []string{"pair:", "ip:"} {
		if !strings.HasPrefix(keys[i], prefix) {
			t.Errorf("key %d = %q, want prefix %q", i, keys[i], prefix)
		}
	}
	if keys[2] != "global:login" {
		t.Fatalf("global key = %q", keys[2])
	}
	trimmed := loginKeys("203.0.113.4", "Alice")
	for i := range keys {
		if keys[i] != trimmed[i] {
			t.Fatalf("username trimming differs at key %d", i)
		}
	}
	lowercase := loginKeys("203.0.113.4", "alice")
	if keys[0] == lowercase[0] {
		t.Fatal("case-sensitive usernames must not share a pair limiter key")
	}
	for _, key := range keys {
		if strings.HasPrefix(key, "user:") {
			t.Fatalf("global username limiter key %q can be abused for account lockout", key)
		}
	}
}
