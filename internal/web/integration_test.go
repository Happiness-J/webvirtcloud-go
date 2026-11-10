package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"webvirtcloud-go/internal/audit"
	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	consolepkg "webvirtcloud-go/internal/console"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/jobs"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

var csrfPattern = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

type testHTTP struct {
	t      *testing.T
	base   string
	client *http.Client
}

func (c *testHTTP) request(method, path string, form url.Values) (int, string, string) {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, string(data), resp.Request.URL.RequestURI()
}

func (c *testHTTP) csrf(path string) string {
	c.t.Helper()
	status, body, _ := c.request(http.MethodGet, path, nil)
	if status != http.StatusOK {
		c.t.Fatalf("GET %s status %d", path, status)
	}
	match := csrfPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		c.t.Fatalf("no csrf token in %s", path)
	}
	return match[1]
}

func newTestClient(t *testing.T, base string) *testHTTP {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &testHTTP{t: t, base: base, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

func loginTestUser(t *testing.T, base, username, password string) *testHTTP {
	t.Helper()
	client := newTestClient(t, base)
	csrf := client.csrf("/login")
	status, _, final := client.request(http.MethodPost, "/login", url.Values{
		"_csrf": {csrf}, "username": {username}, "password": {password},
	})
	if status != http.StatusOK || final != "/" {
		t.Fatalf("login status=%d final=%s", status, final)
	}
	return client
}

func newIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	baseURL := os.Getenv("WVC_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("WVC_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "wvc_web_test_" + hex.EncodeToString(suffix[:])
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, baseURL)
		if err != nil {
			t.Logf("cleanup connect: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
			t.Logf("cleanup schema: %v", err)
		}
	})

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWebIntegration(t *testing.T) {
	fakeVirsh, err := filepath.Abs("../../testdata/fake-virsh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fakeVirsh); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := newIntegrationStore(t)
	keyDir := t.TempDir()
	if err := os.Chmod(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.LoadOrCreate(filepath.Join(keyDir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionTTL: time.Hour, CommandTimeout: 30 * time.Second, MaxTimeout: time.Minute,
		MasterKeyFile: "unused", VirshBin: fakeVirsh, QemuImgBin: "qemu-img",
		QemuIOBin: "qemu-io", QemuNbdBin: "qemu-nbd", MaxOutputBytes: 1024 * 1024,
		MaxArgs: 128, MaxStdinBytes: 1024 * 1024, Workers: 1, ConsoleTokenTTL: time.Minute,
		LoginWindow: time.Minute, LoginMaxFailures: 10, LoginGlobalMaxFailures: 50,
	}
	runner := command.NewRunner(cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(cfg, db, cipher, runner, audit.New(db, log), log)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	adminName := "admin_" + strings.ReplaceAll(id.New(""), "-", "")[:8]
	viewerName := "viewer_" + strings.ReplaceAll(id.New(""), "-", "")[:8]
	adminPassword := "admin-test-password"
	viewerPassword := "viewer-test-password"
	for _, u := range []struct{ name, password, role string }{
		{adminName, adminPassword, "admin"}, {viewerName, viewerPassword, "viewer"},
	} {
		hash, err := auth.HashPassword(u.password)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CreateUser(ctx, store.User{ID: id.New("user"), Username: u.name, PasswordHash: hash, Role: u.role}); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := db.GetUserByUsername(ctx, adminName)
	if err != nil {
		t.Fatal(err)
	}

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		jobs.New(cfg, db, cipher, runner, log).Run(workerCtx)
	}()
	defer func() {
		stopWorker()
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("worker did not stop")
		}
	}()

	client := loginTestUser(t, ts.URL, adminName, adminPassword)
	status, _, _ := client.request(http.MethodPost, "/hosts/new", url.Values{
		"name": {id.New("host")}, "uri": {"qemu:///system"}, "enabled": {"1"},
	})
	if status != http.StatusForbidden {
		t.Fatalf("POST without CSRF status=%d, want 403", status)
	}
	csrf := client.csrf("/")
	hostName := id.New("host")
	status, _, final := client.request(http.MethodPost, "/hosts/new", url.Values{
		"_csrf": {csrf}, "name": {hostName}, "uri": {"qemu:///system"}, "enabled": {"1"},
	})
	if status != http.StatusOK {
		t.Fatalf("create host status=%d final=%s", status, final)
	}
	hosts, err := db.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var host store.Host
	for _, h := range hosts {
		if h.Name == hostName {
			host = h
		}
	}
	if host.ID == "" {
		t.Fatal("host was not created")
	}
	secondHostName := hostName + "-second"
	status, _, final = client.request(http.MethodPost, "/hosts/new", url.Values{
		"_csrf": {csrf}, "name": {secondHostName}, "uri": {"qemu:///system"}, "enabled": {"1"},
	})
	if status != http.StatusOK {
		t.Fatalf("create second host status=%d final=%s", status, final)
	}
	hosts, err = db.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var secondHost store.Host
	for _, h := range hosts {
		if h.Name == secondHostName {
			secondHost = h
		}
	}
	if secondHost.ID == "" || secondHost.ID == host.ID {
		t.Fatal("second host was not created independently")
	}
	passwordToken, err := server.tokens.Issue(host.ID, "web-prod", admin.ID, consolepkg.ScopePassword, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	passwordPath := "/console/" + url.PathEscape(host.ID) + "/web-prod/password?token=" + url.QueryEscape(passwordToken)
	status, body, _ := client.request(http.MethodGet, passwordPath, nil)
	if status != http.StatusOK || !strings.Contains(body, "vnc-secret") {
		t.Fatalf("console password status=%d body=%s", status, body)
	}
	status, body, _ = client.request(http.MethodGet, passwordPath, nil)
	if status != http.StatusUnauthorized || !strings.Contains(body, "already used") {
		t.Fatalf("replayed console token status=%d body=%s", status, body)
	}
	failureToken, err := server.tokens.Issue(host.ID, "web-prod", admin.ID, consolepkg.ScopePassword, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	failurePath := "/console/" + url.PathEscape(host.ID) + "/web-prod/password?token=" + url.QueryEscape(failureToken)
	disabledHost := host
	disabledHost.Enabled = false
	if err := db.UpdateHost(ctx, disabledHost); err != nil {
		t.Fatal(err)
	}
	status, body, _ = client.request(http.MethodGet, failurePath, nil)
	if status != http.StatusBadGateway {
		t.Fatalf("console password with disabled host status=%d body=%s", status, body)
	}
	if err := db.UpdateHost(ctx, host); err != nil {
		t.Fatal(err)
	}
	status, body, _ = client.request(http.MethodGet, failurePath, nil)
	if status != http.StatusOK || !strings.Contains(body, "vnc-secret") {
		t.Fatalf("console token was consumed by a failed request: status=%d body=%s", status, body)
	}
	status, body, _ = client.request(http.MethodGet, "/vms", nil)
	if status != http.StatusOK || !strings.Contains(body, "web-prod") ||
		!strings.Contains(body, hostName) || !strings.Contains(body, secondHostName) {
		t.Fatalf("VM list status=%d body must group VMs under both hosts", status)
	}
	status, body, _ = client.request(http.MethodGet, "/hosts/"+host.ID+"/vms/web-prod", nil)
	if status != http.StatusOK || !strings.Contains(body, "电源与生命周期") {
		t.Fatalf("VM detail status=%d", status)
	}
	opsPath := "/hosts/" + host.ID + "/ops"
	status, body, _ = client.request(http.MethodGet, opsPath, nil)
	if status != http.StatusOK || !strings.Contains(body, "运维工作台") ||
		!strings.Contains(body, "虚拟机资源与批量操作") || !strings.Contains(body, "12.5%") {
		t.Fatalf("operations workbench status=%d body=%s", status, body)
	}

	status, _, _ = client.request(http.MethodPost, "/hosts/"+host.ID+"/vms/web-prod/action", url.Values{
		"_csrf": {csrf}, "action": {"destroy"},
	})
	if status != http.StatusOK {
		t.Fatalf("destructive action without confirm status=%d", status)
	}

	commandPath := "/hosts/" + host.ID + "/command"
	status, body, _ = client.request(http.MethodGet, commandPath, nil)
	if status != http.StatusOK || !strings.Contains(body, "command-center") || !strings.Contains(body, "官方命令目录") ||
		!strings.Contains(body, `max="60"`) {
		t.Fatalf("command page status=%d body=%s", status, body)
	}
	status, body, _ = client.request(http.MethodGet, commandPath+"/catalog", nil)
	if status != http.StatusOK || !strings.Contains(body, `"command":"snapshot-create-as"`) ||
		!strings.Contains(body, `"command":"bitmap"`) || !strings.Contains(body, `"literal":"--"`) ||
		!strings.Contains(body, `"command":"find-pool-sources-as"`) ||
		!strings.Contains(body, `"tool":"qemu-storage-daemon"`) ||
		!strings.Contains(body, `"--handshake-limit"`) || !strings.Contains(body, `"options"`) {
		t.Fatalf("command catalog status=%d body=%s", status, body)
	}
	status, _, final = client.request(http.MethodPost, commandPath, url.Values{
		"_csrf": {csrf}, "tool": {"virsh"}, "args": {"list --all"}, "timeout": {"30"}, "confirm": {"yes"},
	})
	if status != http.StatusOK || !strings.HasPrefix(final, "/jobs/job_") {
		t.Fatalf("command status=%d final=%s", status, final)
	}
	jobID := strings.TrimPrefix(strings.Split(final, "?")[0], "/jobs/")
	var job store.Job
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err = db.GetJob(ctx, jobID)
		if err == nil && (job.Status == "succeeded" || job.Status == "failed" || job.Status == "cancelled") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != "succeeded" || !strings.Contains(job.Output, "web-prod") {
		t.Fatalf("job status=%s output=%q error=%q", job.Status, job.Output, job.Error)
	}
	status, body, _ = client.request(http.MethodGet, "/api/jobs/"+jobID, nil)
	if status != http.StatusOK || !strings.Contains(body, "succeeded") ||
		!strings.Contains(body, `"started_at"`) || !strings.Contains(body, `"finished_at"`) {
		t.Fatalf("job API status=%d body=%s", status, body)
	}

	storagePath := "/hosts/" + host.ID + "/storage?pool=default"
	status, body, _ = client.request(http.MethodGet, storagePath, nil)
	if status != http.StatusOK || !strings.Contains(body, "存储运维") || !strings.Contains(body, "调整卷容量") {
		t.Fatalf("storage operations page status=%d body=%s", status, body)
	}
	var operationJobs []string
	for _, form := range []struct {
		path string
		form url.Values
	}{
		{
			path: "/hosts/" + host.ID + "/storage/action",
			form: url.Values{"_csrf": {csrf}, "pool": {"default"}, "action": {"volume-resize"}, "volume": {"web.qcow2"}, "resize_mode": {"grow"}, "capacity": {"10G"}},
		},
		{
			path: "/hosts/" + host.ID + "/networks/action",
			form: url.Values{"_csrf": {csrf}, "network": {"default"}, "action": {"net-autostart-on"}},
		},
		{
			path: commandPath[:strings.Index(commandPath, "/command")] + "/vms/web-prod/disk-resize",
			form: url.Values{"_csrf": {csrf}, "disk_target": {"vda"}, "disk_mode": {"grow"}, "disk_amount": {"10G"}},
		},
		{
			path: "/hosts/" + host.ID + "/vms/web-prod/resource",
			form: url.Values{"_csrf": {csrf}, "kind": {"cpu"}, "scope": {"config"}, "cpu_count": {"8"}},
		},
		{
			path: "/hosts/" + host.ID + "/batch",
			form: url.Values{"_csrf": {csrf}, "action": {"shutdown"}, "confirm": {"yes"}, "vm": {"web-prod"}},
		},
	} {
		status, _, final = client.request(http.MethodPost, form.path, form.form)
		if status != http.StatusOK || !strings.HasPrefix(final, "/jobs/job_") {
			t.Fatalf("operation POST %s status=%d final=%s", form.path, status, final)
		}
		operationJobs = append(operationJobs, strings.TrimPrefix(strings.Split(final, "?")[0], "/jobs/"))
	}
	for _, operationID := range operationJobs {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			job, err = db.GetJob(ctx, operationID)
			if err == nil && (job.Status == "succeeded" || job.Status == "failed" || job.Status == "cancelled") {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if job.Status != "succeeded" {
			t.Fatalf("operation job %s status=%s output=%q error=%q", operationID, job.Status, job.Output, job.Error)
		}
	}

	status, _, final = client.request(http.MethodPost, commandPath, url.Values{
		"_csrf": {csrf}, "tool": {"virsh"}, "args": {"slow"}, "timeout": {"60"}, "confirm": {"yes"},
	})
	if status != http.StatusOK || !strings.HasPrefix(final, "/jobs/job_") {
		t.Fatalf("slow command status=%d final=%s", status, final)
	}
	slowID := strings.TrimPrefix(strings.Split(final, "?")[0], "/jobs/")
	time.Sleep(200 * time.Millisecond)
	status, _, _ = client.request(http.MethodPost, "/jobs/"+slowID+"/cancel", url.Values{"_csrf": {csrf}})
	if status != http.StatusOK {
		t.Fatalf("cancel job status=%d", status)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err = db.GetJob(ctx, slowID)
		if err == nil && job.Status == "cancelled" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != "cancelled" {
		t.Fatalf("cancelled job status=%s error=%q", job.Status, job.Error)
	}

	viewer := loginTestUser(t, ts.URL, viewerName, viewerPassword)
	status, _, _ = viewer.request(http.MethodGet, commandPath, nil)
	if status != http.StatusForbidden {
		t.Fatalf("viewer command center status=%d", status)
	}
	status, _, _ = viewer.request(http.MethodGet, "/jobs/"+jobID, nil)
	if status != http.StatusForbidden {
		t.Fatalf("viewer accessed another user's job: %d", status)
	}
	status, _, _ = viewer.request(http.MethodGet, "/jobs", nil)
	if status != http.StatusOK {
		t.Fatalf("viewer jobs status=%d", status)
	}
	status, body, _ = viewer.request(http.MethodGet, "/hosts/"+host.ID+"/vms/web-prod", nil)
	if status != http.StatusOK || strings.Contains(body, "电源与生命周期") {
		t.Fatalf("viewer VM detail status=%d must not expose mutation controls", status)
	}
	status, body, _ = viewer.request(http.MethodGet, opsPath, nil)
	if status != http.StatusOK || strings.Contains(body, "执行批量任务") || strings.Contains(body, "提交 vCPU 调整") {
		t.Fatalf("viewer operations workbench status=%d must not expose mutation controls", status)
	}

	audits, err := db.RecentAudit(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range audits {
		if entry.Action == "job.finished" && entry.ActorID == admin.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("job completion audit not found")
	}

	accountCSRF := viewer.csrf("/account/password")
	status, _, final = viewer.request(http.MethodPost, "/account/password", url.Values{
		"_csrf": {accountCSRF}, "current_password": {"wrong-password"}, "new_password": {"viewer-new-password"}, "confirm_password": {"viewer-new-password"},
	})
	if status != http.StatusOK || !strings.Contains(final, "/account/password") {
		t.Fatalf("wrong current password status=%d final=%s", status, final)
	}
	status, _, final = viewer.request(http.MethodPost, "/account/password", url.Values{
		"_csrf": {accountCSRF}, "current_password": {viewerPassword}, "new_password": {"viewer-new-password"}, "confirm_password": {"viewer-new-password"},
	})
	if status != http.StatusOK || !strings.HasPrefix(final, "/login") {
		t.Fatalf("password change status=%d final=%s", status, final)
	}
	status, _, final = viewer.request(http.MethodGet, "/", nil)
	if status != http.StatusOK || !strings.HasPrefix(final, "/login") {
		t.Fatalf("old session remained valid: status=%d final=%s", status, final)
	}
	_ = loginTestUser(t, ts.URL, viewerName, "viewer-new-password")
	updated, err := db.GetUserByUsername(ctx, viewerName)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword("viewer-new-password", updated.PasswordHash) {
		t.Fatal("stored password was not updated")
	}
}

func TestReadinessUsesEnabledHostExecutionMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := newIntegrationStore(t)
	keyDir := t.TempDir()
	if err := os.Chmod(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.LoadOrCreate(filepath.Join(keyDir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SSHBin: fakeSSH, VirshBin: "wvc-missing-virsh", QemuImgBin: "wvc-missing-qemu-img",
		QemuIOBin: "wvc-missing-qemu-io", QemuNbdBin: "wvc-missing-qemu-nbd",
		MaxOutputBytes: 1024 * 1024, MaxArgs: 128, MaxStdinBytes: 1024 * 1024,
		ConsoleTokenTTL: time.Minute,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(cfg, db, cipher, command.NewRunner(cfg), audit.New(db, log), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkerHeartbeat(ctx, "readiness-test"); err != nil {
		t.Fatal(err)
	}
	remoteID := id.New("host")
	remoteURI, err := cipher.SealFor("host:"+remoteID, "qemu+ssh://root@kvm.example/system")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateHost(ctx, store.Host{ID: remoteID, Name: "remote-kvm", URICiphertext: remoteURI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	check := func() (int, string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ready(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return recorder.Code, recorder.Body.String()
	}
	if status, body := check(); status != http.StatusOK || body != "ready\n" {
		t.Fatalf("remote-only readiness = %d %q", status, body)
	}

	localID := id.New("host")
	localURI, err := cipher.SealFor("host:"+localID, "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateHost(ctx, store.Host{ID: localID, Name: "local-kvm", URICiphertext: localURI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	status, body := check()
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "requires virsh") {
		t.Fatalf("local host without virsh readiness = %d %q", status, body)
	}

	localHost, err := db.GetHost(ctx, localID)
	if err != nil {
		t.Fatal(err)
	}
	localHost.Enabled = false
	if err := db.UpdateHost(ctx, localHost); err != nil {
		t.Fatal(err)
	}
	invalidID := id.New("host")
	if err := db.CreateHost(ctx, store.Host{
		ID: invalidID, Name: "invalid-kvm", URICiphertext: "v2:not-valid-ciphertext", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	status, body = check()
	if status != http.StatusServiceUnavailable || body != "enabled host configuration is invalid\n" {
		t.Fatalf("invalid enabled host readiness = %d %q", status, body)
	}
	if strings.Contains(body, "invalid-kvm") || strings.Contains(body, "not-valid") {
		t.Fatalf("readiness response leaked host configuration: %q", body)
	}
}
