package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/secret"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	baseURL := os.Getenv("WVC_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("WVC_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema := "wvc_store_test_" + randomHex(t, 8)
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
	db, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	cipher, err := secret.New(bytes.Repeat([]byte{0x5a}, 32))
	if err != nil {
		t.Fatal(err)
	}
	db.SetCipher(cipher)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(buf)
}

func createTestUser(t *testing.T, db *Store, role string) User {
	t.Helper()
	user := User{ID: id.New("user"), Username: id.New("name"), PasswordHash: "unused", Role: role}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}

func createTestHost(t *testing.T, db *Store) Host {
	t.Helper()
	hostID := id.New("host")
	encrypted, err := db.cipher.SealFor("host:"+hostID, "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	host := Host{ID: hostID, Name: id.New("hostname"), URICiphertext: encrypted, Enabled: true}
	if err := db.CreateHost(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	return host
}

func TestReserveLoginAttemptEnforcesLimits(t *testing.T) {
	t.Run("pair and user", func(t *testing.T) {
		db := newTestStore(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		keys := []string{"pair:test", "ip:test", "user:test", "global:login"}
		for i := 0; i < 3; i++ {
			allowed, err := db.ReserveLoginAttempt(ctx, keys, 3, 100, 100, time.Minute)
			if err != nil || !allowed {
				t.Fatalf("reservation %d = %v, %v", i+1, allowed, err)
			}
		}
		allowed, err := db.ReserveLoginAttempt(ctx, keys, 3, 100, 100, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Fatal("pair/user limit was not enforced")
		}
	})

	t.Run("IP", func(t *testing.T) {
		db := newTestStore(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for i := 0; i < 3; i++ {
			keys := []string{fmt.Sprintf("pair:%d", i), "ip:test", fmt.Sprintf("user:%d", i), "global:login"}
			allowed, err := db.ReserveLoginAttempt(ctx, keys, 100, 3, 100, time.Minute)
			if err != nil || !allowed {
				t.Fatalf("reservation %d = %v, %v", i+1, allowed, err)
			}
		}
		allowed, err := db.ReserveLoginAttempt(ctx, []string{"pair:other", "ip:test", "user:other", "global:login"}, 100, 3, 100, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Fatal("IP-wide limit was not enforced")
		}
	})

	t.Run("global observability does not lock out legitimate users", func(t *testing.T) {
		db := newTestStore(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for i := 0; i < 3; i++ {
			allowed, err := db.ReserveLoginAttempt(ctx, []string{"global:login"}, 100, 100, 3, time.Minute)
			if err != nil || !allowed {
				t.Fatalf("reservation %d = %v, %v", i+1, allowed, err)
			}
		}
		allowed, err := db.ReserveLoginAttempt(ctx, []string{"global:login"}, 100, 100, 3, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatal("global counter must not hard-block independent login attempts")
		}
		allowed, err = db.ReserveLoginAttempt(ctx,
			[]string{"pair:other", "ip:192.0.2.10", "user:other", "global:login"},
			100, 100, 3, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatal("global counter blocked a fresh IP/user/pair")
		}
		var failures int
		if err := db.pool.QueryRow(ctx, `SELECT failures FROM login_attempts WHERE key='global:login'`).Scan(&failures); err != nil {
			t.Fatal(err)
		}
		if failures != 5 {
			t.Fatalf("global failure counter = %d, want 5", failures)
		}
	})
}

func TestCompleteLoginAttemptRefundsOnlyReservation(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	keys := []string{"pair:test", "ip:test", "user:test", "global:login"}
	for i := 0; i < 2; i++ {
		allowed, err := db.ReserveLoginAttempt(ctx, keys, 5, 5, 5, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("reservation %d = %v, %v", i+1, allowed, err)
		}
	}
	if err := db.CompleteLoginAttempt(ctx, keys); err != nil {
		t.Fatal(err)
	}
	var pairCount, ipCount, globalCount int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM login_attempts WHERE key IN ('pair:test','user:test')`).Scan(&pairCount); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT failures FROM login_attempts WHERE key='ip:test'`).Scan(&ipCount); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT failures FROM login_attempts WHERE key='global:login'`).Scan(&globalCount); err != nil {
		t.Fatal(err)
	}
	if pairCount != 0 {
		t.Fatalf("pair/user counters = %d, want 0", pairCount)
	}
	if ipCount != 1 || globalCount != 1 {
		t.Fatalf("IP/global counters = %d/%d, want 1/1", ipCount, globalCount)
	}
}

func TestReserveLoginAttemptConcurrentLimit(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	keys := []string{"pair:concurrent", "ip:concurrent", "user:concurrent", "global:concurrent"}
	const calls, limit = 32, 5
	start := make(chan struct{})
	results := make(chan bool, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			allowed, err := db.ReserveLoginAttempt(ctx, keys, limit, 100, 100, time.Minute)
			results <- allowed
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	allowedCount := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for allowed := range results {
		if allowed {
			allowedCount++
		}
	}
	if allowedCount != limit {
		t.Fatalf("allowed reservations = %d, want %d", allowedCount, limit)
	}
}

func TestLoginAttemptReserveCompleteConcurrent(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	keys := []string{"pair:concurrent", "ip:concurrent", "user:concurrent", "global:login"}
	const workers = 12
	const rounds = 20
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < rounds; j++ {
				allowed, err := db.ReserveLoginAttempt(ctx, keys, 1000, 1000, 1000, time.Minute)
				if err != nil {
					errCh <- err
					return
				}
				if !allowed {
					errCh <- errors.New("reservation unexpectedly rejected")
					return
				}
				if err := db.CompleteLoginAttempt(ctx, keys); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent reserve/complete: %v", err)
	}
}

func TestCreateSessionPrunesExpiredAndCapsPerUser(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "viewer")
	expiredHash := "expired-token"
	if err := db.CreateSession(ctx, expiredHash, user.ID, user.PasswordHash, "csrf-old", time.Now().Add(-time.Minute), "127.0.0.1", "test", 2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		hash := fmt.Sprintf("session-token-%d", i)
		if err := db.CreateSession(ctx, hash, user.ID, user.PasswordHash, "csrf", time.Now().Add(time.Hour), "127.0.0.1", "test", 2); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, user.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("session count = %d, want 2", count)
	}
	var expired int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash=$1`, expiredHash).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatal("expired session was not pruned")
	}
	var newest int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash='session-token-3'`).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if newest != 1 {
		t.Fatal("newest session was evicted by the cap")
	}
}

func TestCreateSessionConcurrentLimit(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "viewer")
	const calls, limit = 16, 3
	start := make(chan struct{})
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			errs <- db.CreateSession(ctx, fmt.Sprintf("concurrent-%d", index), user.ID, user.PasswordHash, "csrf", time.Now().Add(time.Hour), "127.0.0.1", "test", limit)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, user.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != limit {
		t.Fatalf("session count = %d, want %d", count, limit)
	}
}

func TestClaimConsoleTokenIsAtomicAndPurgeable(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const calls = 32
	start := make(chan struct{})
	results := make(chan bool, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := db.ClaimConsoleToken(ctx, "shared-console-token", time.Now().Add(time.Minute))
			results <- claimed
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	claims := 0
	for claimed := range results {
		if claimed {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("successful claims = %d, want 1", claims)
	}

	if claimed, err := db.ClaimConsoleToken(ctx, "expired-console-token", time.Now().Add(-time.Minute)); err != nil || !claimed {
		t.Fatalf("insert expired token: %v, %v", claimed, err)
	}
	removed, err := db.PurgeExpiredConsoleTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("purged console tokens = %d, want 1", removed)
	}
}

func TestCreateSessionRejectsChangedPassword(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "viewer")
	oldHash := user.PasswordHash
	if err := db.SetPassword(ctx, user.ID, "new-password-hash"); err != nil {
		t.Fatal(err)
	}
	err := db.CreateSession(ctx, "stale-session", user.ID, oldHash, "csrf", time.Now().Add(time.Hour), "127.0.0.1", "test", 2)
	if !errors.Is(err, ErrSessionCredentialsChanged) {
		t.Fatalf("CreateSession() error = %v, want ErrSessionCredentialsChanged", err)
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, user.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sessions created with stale password = %d, want 0", count)
	}
	if err := db.CreateSession(ctx, "current-session", user.ID, "new-password-hash", "csrf", time.Now().Add(time.Hour), "127.0.0.1", "test", 2); err != nil {
		t.Fatalf("CreateSession() with current password = %v", err)
	}
}

func TestJobDedupeKeyIsKeyedAndFieldBound(t *testing.T) {
	keyA := bytes.Repeat([]byte{0x11}, 32)
	keyB := bytes.Repeat([]byte{0x22}, 32)
	base := jobDedupeKey(keyA, "command", "user", "host", "virsh", `["list"]`, "")
	if base != jobDedupeKey(keyA, "command", "user", "host", "virsh", `["list"]`, "") {
		t.Fatal("dedupe key is not deterministic")
	}
	if base == jobDedupeKey(keyB, "command", "user", "host", "virsh", `["list"]`, "") {
		t.Fatal("dedupe key does not depend on the server key")
	}
	if base == jobDedupeKey(keyA, "vm-action", "user", "host", "virsh", `["list"]`, "") {
		t.Fatal("dedupe key does not depend on job kind")
	}
	if jobDedupeKey(keyA, "command", "user", "host", "virsh", "ab", "c") == jobDedupeKey(keyA, "command", "user", "host", "virsh", "a", "bc") {
		t.Fatal("dedupe key is not field-bound")
	}
}

func TestJobEncryptionAndWorkerOwnership(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: "list", Tool: "virsh",
		ArgsJSON: `["list","--all"]`, Stdin: "stdin-secret", ReadOnly: true, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	var argsPlain, stdinPlain, argsCipher, stdinCipher string
	if err := db.pool.QueryRow(ctx, `SELECT args_json::text, stdin, args_ciphertext, stdin_ciphertext FROM jobs WHERE id=$1`, jobID).
		Scan(&argsPlain, &stdinPlain, &argsCipher, &stdinCipher); err != nil {
		t.Fatal(err)
	}
	if argsPlain != "[]" || stdinPlain != "" || argsCipher == "" || stdinCipher == "" {
		t.Fatalf("plaintext job payload was not scrubbed: args=%q stdin=%q args_cipher=%v stdin_cipher=%v", argsPlain, stdinPlain, argsCipher != "", stdinCipher != "")
	}

	job, err := db.ClaimJob(ctx, "worker-a")
	if err != nil || job == nil {
		t.Fatalf("ClaimJob = %#v, %v", job, err)
	}
	if !strings.Contains(job.ArgsJSON, `"list"`) || job.Stdin != "stdin-secret" {
		t.Fatalf("claimed job payload = %#v", job)
	}
	if active, err := db.HeartbeatJob(ctx, job.ID, "worker-b"); err != nil || active {
		t.Fatalf("wrong worker heartbeat = %v, %v", active, err)
	}
	if finished, err := db.FinishJob(ctx, job.ID, "worker-b", "succeeded", "wrong", ""); err != nil || finished {
		t.Fatalf("wrong worker finish = %v, %v", finished, err)
	}
	if finished, err := db.FinishJob(ctx, job.ID, "worker-a", "succeeded", "visible output", ""); err != nil || !finished {
		t.Fatalf("owner finish = %v, %v", finished, err)
	}
	var outputPlain, errorPlain, outputCipher string
	if err := db.pool.QueryRow(ctx, `SELECT output, error, output_ciphertext FROM jobs WHERE id=$1`, jobID).
		Scan(&outputPlain, &errorPlain, &outputCipher); err != nil {
		t.Fatal(err)
	}
	if outputPlain != "" || errorPlain != "" || outputCipher == "" {
		t.Fatalf("job result was not encrypted: output=%q error=%q cipher=%v", outputPlain, errorPlain, outputCipher != "")
	}
	finishedJob, err := db.GetJob(ctx, jobID)
	if err != nil || finishedJob.Output != "visible output" || finishedJob.Status != "succeeded" {
		t.Fatalf("GetJob = %#v, %v", finishedJob, err)
	}
}

func TestConcurrentEnqueueDedupe(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	const workers = 16
	start := make(chan struct{})
	results := make([]error, workers)
	jobIDs := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			jobIDs[index] = id.New("job")
			results[index] = db.EnqueueJob(ctx, Job{
				ID: jobIDs[index], UserID: user.ID, HostID: host.ID, Kind: "command", Label: "info", Tool: "virsh",
				ArgsJSON: `["list"]`, ReadOnly: true, TimeoutSeconds: 30,
			})
		}(i)
	}
	close(start)
	wg.Wait()
	successes := 0
	existing := ""
	for i, err := range results {
		if err == nil {
			successes++
			existing = jobIDs[i]
			continue
		}
		var duplicate *DuplicateJobError
		if !errors.As(err, &duplicate) {
			t.Fatalf("worker %d error = %v, want DuplicateJobError", i, err)
		}
		if duplicate.ExistingID == "" {
			t.Fatal("duplicate error did not identify the active job")
		}
	}
	if successes != 1 {
		t.Fatalf("successful enqueues = %d, want 1", successes)
	}
	for i, err := range results {
		if err != nil {
			var duplicate *DuplicateJobError
			_ = errors.As(err, &duplicate)
			if duplicate.ExistingID != existing {
				t.Fatalf("worker %d duplicate id = %q, want %q", i, duplicate.ExistingID, existing)
			}
		}
	}
}

func TestRecoverStaleJobsAndOwnershipCAS(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "operator")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "vm-action", Label: "start", Tool: "virsh",
		ArgsJSON: `["start","vm"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if job, err := db.ClaimJob(ctx, "worker-old"); err != nil || job == nil {
		t.Fatalf("ClaimJob = %#v, %v", job, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at=now()-interval '10 minutes' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.RecoverStaleJobs(ctx, 5*time.Minute)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverStaleJobs = %d, %v", recovered, err)
	}
	if finished, err := db.FinishJob(ctx, jobID, "worker-old", "succeeded", "late", ""); err != nil || finished {
		t.Fatalf("late FinishJob = %v, %v", finished, err)
	}
	job, err := db.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" || !strings.Contains(job.Error, "worker lost") || job.Output != "" {
		t.Fatalf("recovered job = %#v", job)
	}
	var plainError string
	if err := db.pool.QueryRow(ctx, `SELECT error FROM jobs WHERE id=$1`, jobID).Scan(&plainError); err != nil {
		t.Fatal(err)
	}
	if plainError != "" {
		t.Fatalf("recovery error was stored in plaintext: %q", plainError)
	}
	var outbox int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_outbox WHERE source_id=$1`, "job-recovered:"+jobID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("recovered job audit outbox rows = %d, want 1", outbox)
	}
	if processed, err := db.ProcessAuditOutbox(ctx, 10); err != nil || processed == 0 {
		t.Fatalf("ProcessAuditOutbox = %d, %v", processed, err)
	}
	audits, err := db.RecentAudit(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range audits {
		if entry.Action == "job.recovered" {
			if recoveredID, _ := entry.Detail["job_id"].(string); recoveredID == jobID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("stale job recovery audit event not found")
	}
}

func TestRecoverStaleJobsDoesNotLockJoinedUsers(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "operator")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "vm-action", Label: "start", Tool: "virsh",
		ArgsJSON: `["start","vm"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if job, err := db.ClaimJob(ctx, "worker-old"); err != nil || job == nil {
		t.Fatalf("ClaimJob = %#v, %v", job, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at=now()-interval '10 minutes' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}

	lockTx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback(context.Background())
	if _, err := lockTx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user.ID); err != nil {
		t.Fatal(err)
	}

	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, 2*time.Second)
	defer recoveryCancel()
	recovered, err := db.RecoverStaleJobs(recoveryCtx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleJobs while user row is locked = %d, %v", recovered, err)
	}
	if recovered != 1 {
		t.Fatalf("RecoverStaleJobs while user row is locked = %d, want 1", recovered)
	}
}

func TestLegacyCommandJobSummariesAreHiddenFromNonAdminLists(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "operator")
	host := createTestHost(t, db)
	jobID := id.New("job")
	const label = "virsh destroy vm --password hunter2"
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: label, Tool: "virsh",
		ArgsJSON: `["destroy","vm","--password","hunter2"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET status='failed', error='legacy secret in error' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}

	jobs, err := db.ListJobsPage(ctx, user.ID, false, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobsPage returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].Label != "virsh [敏感参数已隐藏]" || jobs[0].Error != "" {
		t.Fatalf("legacy command job leaked in paginated list: %#v", jobs[0])
	}
	hostJobs, err := db.ListJobsByHost(ctx, user.ID, false, host.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostJobs) != 1 || hostJobs[0].Label != "virsh [敏感参数已隐藏]" || hostJobs[0].Error != "" {
		t.Fatalf("legacy command job leaked in host list: %#v", hostJobs)
	}
	adminJobs, err := db.ListJobsPage(ctx, user.ID, true, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminJobs) != 1 || adminJobs[0].Label != label || adminJobs[0].Error != "legacy secret in error" {
		t.Fatalf("admin lost access to legacy command summary: %#v", adminJobs)
	}
}

func TestRecoverStaleLegacyCommandJobHidesAuditLabel(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "operator")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "command",
		Label: "virsh destroy vm --password hunter2", Tool: "virsh",
		ArgsJSON: `["destroy","vm","--password","hunter2"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if job, err := db.ClaimJob(ctx, "worker-old"); err != nil || job == nil || job.ID != jobID {
		t.Fatalf("ClaimJob = %#v, %v", job, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at=now()-interval '10 minutes' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if recovered, err := db.RecoverStaleJobs(ctx, 5*time.Minute); err != nil || recovered != 1 {
		t.Fatalf("RecoverStaleJobs = %d, %v", recovered, err)
	}
	var label string
	if err := db.pool.QueryRow(ctx, `SELECT detail->>'label' FROM audit_outbox WHERE source_id=$1`, "job-recovered:"+jobID).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "virsh [敏感参数已隐藏]" {
		t.Fatalf("recovery audit label = %q, want redacted label", label)
	}
}

func TestDeleteHostProtectsActiveJobs(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	jobID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "host-test", Label: "test", Tool: "virsh",
		ArgsJSON: `["version"]`, ReadOnly: true, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteHost(ctx, host.ID); !errors.Is(err, ErrHostHasActiveJobs) {
		t.Fatalf("DeleteHost with active job = %v", err)
	}
	if active, err := db.RequestCancelJob(ctx, jobID); err != nil || !active {
		t.Fatalf("RequestCancelJob = %v, %v", active, err)
	}
	if err := db.DeleteHost(ctx, host.ID); err != nil {
		t.Fatalf("DeleteHost after cancel = %v", err)
	}
	if _, err := db.GetHost(ctx, host.ID); !IsNotFound(err) {
		t.Fatalf("deleted host lookup = %v", err)
	}
}

func TestUpdateHostURIProtectsActiveJobs(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "operator")
	host := createTestHost(t, db)
	if err := db.EnqueueJob(ctx, Job{
		ID: id.New("job"), UserID: user.ID, HostID: host.ID, Kind: "vm-action", Label: "start",
		Tool: "virsh", ArgsJSON: `["start","vm"]`, TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}

	updated := host
	updated.Name = "renamed"
	if err := db.UpdateHost(ctx, updated); err != nil {
		t.Fatalf("UpdateHost without URI change = %v", err)
	}

	encrypted, err := db.cipher.SealFor("host:"+host.ID, "qemu+ssh://root@other/system")
	if err != nil {
		t.Fatal(err)
	}
	updated.URICiphertext = encrypted
	if err := db.UpdateHost(ctx, updated); !errors.Is(err, ErrHostHasActiveJobs) {
		t.Fatalf("UpdateHost URI change with active job = %v, want ErrHostHasActiveJobs", err)
	}

	if active, err := db.RequestCancelJob(ctx, updatedJobID(t, db, host.ID)); err != nil || !active {
		t.Fatalf("cancel active job = %v, %v", active, err)
	}
	if err := db.UpdateHost(ctx, updated); err != nil {
		t.Fatalf("UpdateHost after cancelling active job = %v", err)
	}
}

func TestEnqueueJobRejectsStaleHostURI(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)

	encrypted, err := db.cipher.SealFor("host:"+host.ID, "qemu+ssh://root@other/system")
	if err != nil {
		t.Fatal(err)
	}
	updated := host
	updated.URICiphertext = encrypted
	if err := db.UpdateHost(ctx, updated); err != nil {
		t.Fatal(err)
	}

	if err := db.EnqueueJob(ctx, Job{
		ID: id.New("job"), UserID: user.ID, HostID: host.ID, Kind: "command", Label: "stale",
		Tool: "virsh", ArgsJSON: `["list"]`, ReadOnly: true, TimeoutSeconds: 30,
		ExpectedHostURICiphertext: host.URICiphertext,
	}); !errors.Is(err, ErrHostChanged) {
		t.Fatalf("EnqueueJob with stale host URI = %v, want ErrHostChanged", err)
	}

	if err := db.EnqueueJob(ctx, Job{
		ID: id.New("job"), UserID: user.ID, HostID: host.ID, Kind: "command", Label: "current",
		Tool: "virsh", ArgsJSON: `["list"]`, ReadOnly: true, TimeoutSeconds: 30,
		ExpectedHostURICiphertext: updated.URICiphertext,
	}); err != nil {
		t.Fatalf("EnqueueJob with current host URI = %v", err)
	}
}

func TestJobResourceSerializationIsStoreInvariant(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)

	enqueue := func(label, args string, readOnly bool, hostID string) string {
		t.Helper()
		jobID := id.New("job")
		if err := db.EnqueueJob(ctx, Job{
			ID: jobID, UserID: user.ID, HostID: hostID, Kind: "command", Label: label,
			Tool: "virsh", ArgsJSON: args, ReadOnly: readOnly, TimeoutSeconds: 30,
		}); err != nil {
			t.Fatal(err)
		}
		return jobID
	}

	firstWrite := enqueue("write-one", `["start","vm-one"]`, false, host.ID)
	secondWrite := enqueue("write-two", `["start","vm-two"]`, false, host.ID)
	var firstKey, secondKey string
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, firstWrite).Scan(&firstKey); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, secondWrite).Scan(&secondKey); err != nil {
		t.Fatal(err)
	}
	wantKey := "host:" + host.ID
	if firstKey != wantKey || secondKey != wantKey {
		t.Fatalf("write resource keys = %q/%q, want %q", firstKey, secondKey, wantKey)
	}

	running, err := db.ClaimJob(ctx, "worker-a")
	if err != nil || running == nil {
		t.Fatalf("claim first write = %#v, %v", running, err)
	}
	if blocked, err := db.ClaimJob(ctx, "worker-b"); err != nil || blocked != nil {
		t.Fatalf("second write was not serialized: %#v, %v", blocked, err)
	}

	readOne := enqueue("read-one", `["list","--all"]`, true, host.ID)
	readTwo := enqueue("read-two", `["list","--inactive"]`, true, host.ID)
	var readKeyOne, readKeyTwo string
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, readOne).Scan(&readKeyOne); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, readTwo).Scan(&readKeyTwo); err != nil {
		t.Fatal(err)
	}
	if readKeyOne != "" || readKeyTwo != "" {
		t.Fatalf("read-only resource keys = %q/%q, want empty", readKeyOne, readKeyTwo)
	}
	readJobOne, err := db.ClaimJob(ctx, "worker-b")
	if err != nil || readJobOne == nil || !readJobOne.ReadOnly {
		t.Fatalf("claim first read-only = %#v, %v", readJobOne, err)
	}
	readJobTwo, err := db.ClaimJob(ctx, "worker-c")
	if err != nil || readJobTwo == nil || !readJobTwo.ReadOnly {
		t.Fatalf("claim second read-only = %#v, %v", readJobTwo, err)
	}
	if finished, err := db.FinishJob(ctx, readJobOne.ID, "worker-b", "succeeded", "", ""); err != nil || !finished {
		t.Fatalf("finish first read-only = %v, %v", finished, err)
	}
	if finished, err := db.FinishJob(ctx, readJobTwo.ID, "worker-c", "succeeded", "", ""); err != nil || !finished {
		t.Fatalf("finish second read-only = %v, %v", finished, err)
	}
	if finished, err := db.FinishJob(ctx, running.ID, "worker-a", "succeeded", "", ""); err != nil || !finished {
		t.Fatalf("finish first write = %v, %v", finished, err)
	}
	nextWrite, err := db.ClaimJob(ctx, "worker-b")
	if err != nil || nextWrite == nil || nextWrite.ID != secondWrite {
		t.Fatalf("claim second write after first finished = %#v, %v", nextWrite, err)
	}

	if err := db.EnqueueJob(ctx, Job{
		ID: id.New("job"), UserID: user.ID, Kind: "command", Label: "unbound",
		Tool: "virsh", ArgsJSON: `["version"]`, TimeoutSeconds: 30,
	}); !errors.Is(err, ErrHostRequired) {
		t.Fatalf("EnqueueJob without a host = %v, want ErrHostRequired", err)
	}
}

func TestEnqueueJobOverridesCallerResourceKey(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)

	readOnlyID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: readOnlyID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: "custom-read",
		Tool: "virsh", ArgsJSON: `["list"]`, ReadOnly: true, TimeoutSeconds: 30,
		ResourceKey: "caller-controlled-read",
	}); err != nil {
		t.Fatal(err)
	}
	var readKey string
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, readOnlyID).Scan(&readKey); err != nil {
		t.Fatal(err)
	}
	if readKey != "" {
		t.Fatalf("read-only resource key = %q, want empty", readKey)
	}

	writeID := id.New("job")
	if err := db.EnqueueJob(ctx, Job{
		ID: writeID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: "custom-write",
		Tool: "virsh", ArgsJSON: `["start","vm"]`, TimeoutSeconds: 30,
		ResourceKey: "caller-controlled-write",
	}); err != nil {
		t.Fatal(err)
	}
	var writeKey string
	if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, writeID).Scan(&writeKey); err != nil {
		t.Fatal(err)
	}
	if want := "host:" + host.ID; writeKey != want {
		t.Fatalf("mutating resource key = %q, want %q", writeKey, want)
	}
}

func TestJobSerializationAllowsDifferentHostsInParallel(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	hostA := createTestHost(t, db)
	hostB := createTestHost(t, db)

	enqueue := func(hostID, label string) string {
		t.Helper()
		jobID := id.New("job")
		if err := db.EnqueueJob(ctx, Job{
			ID: jobID, UserID: user.ID, HostID: hostID, Kind: "command", Label: label,
			Tool: "virsh", ArgsJSON: `["start","vm-one"]`, ReadOnly: false, TimeoutSeconds: 30,
		}); err != nil {
			t.Fatal(err)
		}
		return jobID
	}
	jobA := enqueue(hostA.ID, "host-a-write")
	jobB := enqueue(hostB.ID, "host-b-write")

	first, err := db.ClaimJob(ctx, "worker-a")
	if err != nil || first == nil {
		t.Fatalf("claim first host write = %#v, %v", first, err)
	}
	second, err := db.ClaimJob(ctx, "worker-b")
	if err != nil || second == nil {
		t.Fatalf("claim second host write = %#v, %v; different hosts must run in parallel", second, err)
	}
	if first.ID == second.ID {
		t.Fatalf("both workers claimed the same job %s", first.ID)
	}
	claimed := map[string]bool{first.ID: true, second.ID: true}
	if !claimed[jobA] || !claimed[jobB] {
		t.Fatalf("claimed jobs = %v, want both %s and %s", claimed, jobA, jobB)
	}
	for jobID, hostID := range map[string]string{jobA: hostA.ID, jobB: hostB.ID} {
		var resourceKey string
		if err := db.pool.QueryRow(ctx, `SELECT resource_key FROM jobs WHERE id=$1`, jobID).Scan(&resourceKey); err != nil {
			t.Fatal(err)
		}
		if want := "host:" + hostID; resourceKey != want {
			t.Fatalf("job %s resource key = %q, want %q", jobID, resourceKey, want)
		}
	}
}

func TestJobSerializationMigrationBackfillsLegacyJobs(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)

	// Simulate a database upgraded from a release that had migrations 007 and
	// 008 but not the backfill migration. The legacy rows intentionally have
	// empty resource keys, including two running jobs on the same host.
	if _, err := db.pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=9`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version>=10`); err != nil {
		t.Fatal(err)
	}
	insert := func(id, status string, readOnly bool, hostID any, createdAt string) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, `INSERT INTO jobs(
			id,user_id,host_id,kind,label,tool,args_json,read_only,status,resource_key,created_at,started_at)
			VALUES($1,$2,$3,'command','legacy','virsh','["list"]'::jsonb,$4,$5,'',$6::timestamptz,$6::timestamptz)`,
			id, user.ID, hostID, readOnly, status, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	insert("job-legacy-running-old", "running", false, host.ID, "2026-01-01T00:00:00Z")
	insert("job-legacy-running-duplicate", "running", false, host.ID, "2026-01-01T00:01:00Z")
	insert("job-legacy-queued", "queued", false, host.ID, "2026-01-01T00:02:00Z")
	insert("job-legacy-local", "queued", false, nil, "2026-01-01T00:03:00Z")
	insert("job-legacy-read-only", "queued", true, host.ID, "2026-01-01T00:04:00Z")

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	type legacyState struct {
		status, key, errText string
	}
	state := func(id string) legacyState {
		t.Helper()
		var out legacyState
		if err := db.pool.QueryRow(ctx, `SELECT status,resource_key,error FROM jobs WHERE id=$1`, id).
			Scan(&out.status, &out.key, &out.errText); err != nil {
			t.Fatal(err)
		}
		return out
	}
	wantHostKey := "host:" + host.ID
	if got := state("job-legacy-running-old"); got.status != "running" || got.key != wantHostKey {
		t.Fatalf("oldest legacy running job = %+v", got)
	}
	if got := state("job-legacy-running-duplicate"); got.status != "failed" || got.key != "" || got.errText == "" {
		t.Fatalf("duplicate legacy running job = %+v", got)
	}
	if got := state("job-legacy-queued"); got.status != "queued" || got.key != wantHostKey {
		t.Fatalf("legacy queued job = %+v", got)
	}
	if got := state("job-legacy-local"); got.key != "local:qemu" {
		t.Fatalf("legacy local job = %+v", got)
	}
	if got := state("job-legacy-read-only"); got.key != "" {
		t.Fatalf("legacy read-only job = %+v", got)
	}
}

func TestJobQueueQuotas(t *testing.T) {
	t.Run("per user", func(t *testing.T) {
		db := newTestStore(t)
		db.SetJobLimits(1, 100)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		userOne := createTestUser(t, db, "operator")
		userTwo := createTestUser(t, db, "operator")
		host := createTestHost(t, db)
		enqueue := func(userID, args string) error {
			return db.EnqueueJob(ctx, Job{
				ID: id.New("job"), UserID: userID, HostID: host.ID, Kind: "command", Label: "quota",
				Tool: "virsh", ArgsJSON: args, ReadOnly: true, TimeoutSeconds: 30,
			})
		}
		if err := enqueue(userOne.ID, `["list"]`); err != nil {
			t.Fatal(err)
		}
		if err := enqueue(userOne.ID, `["list","--all"]`); !errors.Is(err, ErrJobQuotaExceeded) {
			t.Fatalf("second job for limited user = %v, want ErrJobQuotaExceeded", err)
		}
		if err := enqueue(userTwo.ID, `["list"]`); err != nil {
			t.Fatalf("quota for one user blocked another user: %v", err)
		}
	})

	t.Run("global", func(t *testing.T) {
		db := newTestStore(t)
		db.SetJobLimits(100, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		userOne := createTestUser(t, db, "operator")
		userTwo := createTestUser(t, db, "operator")
		host := createTestHost(t, db)
		enqueue := func(userID, args string) error {
			return db.EnqueueJob(ctx, Job{
				ID: id.New("job"), UserID: userID, HostID: host.ID, Kind: "command", Label: "quota",
				Tool: "virsh", ArgsJSON: args, ReadOnly: true, TimeoutSeconds: 30,
			})
		}
		if err := enqueue(userOne.ID, `["list"]`); err != nil {
			t.Fatal(err)
		}
		if err := enqueue(userTwo.ID, `["list","--all"]`); !errors.Is(err, ErrJobQuotaExceeded) {
			t.Fatalf("second global job = %v, want ErrJobQuotaExceeded", err)
		}
	})
}

func TestAuditOutboxIdempotentDrainAndStats(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	event := Audit{
		SourceID: "source-one", ActorID: "user", ActorName: "tester", Action: "job.submit",
		HostID: "host", VMName: "vm", Detail: map[string]any{"job": "one"}, RemoteIP: "192.0.2.1",
	}
	for i := 0; i < 2; i++ {
		if err := db.EnqueueAudit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.AuditOutboxStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.Due != 1 || stats.Failed != 0 {
		t.Fatalf("initial outbox stats = %#v", stats)
	}
	if drained, err := db.ProcessAuditOutbox(ctx, 10); err != nil || drained != 1 {
		t.Fatalf("drain outbox = %d, %v", drained, err)
	}
	if drained, err := db.ProcessAuditOutbox(ctx, 10); err != nil || drained != 0 {
		t.Fatalf("second drain outbox = %d, %v", drained, err)
	}
	var auditCount, outboxCount int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE source_id=$1`, event.SourceID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_outbox`).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || outboxCount != 0 {
		t.Fatalf("audit/outbox counts after drain = %d/%d, want 1/0", auditCount, outboxCount)
	}

	duplicate := Audit{SourceID: "source-two", ActorName: "tester", Action: "vm.start"}
	if err := db.AddAudit(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueAudit(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	if drained, err := db.ProcessAuditOutbox(ctx, 10); err != nil || drained != 1 {
		t.Fatalf("drain already-applied outbox = %d, %v", drained, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE source_id=$1`, duplicate.SourceID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_outbox`).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || outboxCount != 0 {
		t.Fatalf("duplicate audit/outbox counts = %d/%d, want 1/0", auditCount, outboxCount)
	}

	leased := Audit{SourceID: "source-leased", ActorName: "tester", Action: "vm.reboot"}
	if err := db.EnqueueAudit(ctx, leased); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE audit_outbox SET next_attempt_at=now()+interval '1 hour' WHERE source_id=$1`, leased.SourceID); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueAudit(ctx, leased); err != nil {
		t.Fatal(err)
	}
	var leasePreserved bool
	if err := db.pool.QueryRow(ctx, `SELECT next_attempt_at > now()+interval '50 minutes' FROM audit_outbox WHERE source_id=$1`, leased.SourceID).Scan(&leasePreserved); err != nil {
		t.Fatal(err)
	}
	if !leasePreserved {
		t.Fatal("duplicate enqueue shortened an existing outbox retry lease")
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM audit_outbox WHERE source_id=$1`, leased.SourceID); err != nil {
		t.Fatal(err)
	}

	retry := Audit{SourceID: "source-three", ActorName: "tester", Action: "vm.stop"}
	if err := db.EnqueueAudit(ctx, retry); err != nil {
		t.Fatal(err)
	}
	cipher := db.cipher
	db.cipher = nil
	if drained, err := db.ProcessAuditOutbox(ctx, 10); err == nil || drained != 0 {
		t.Fatalf("drain without audit key = %d, %v; want retry error", drained, err)
	}
	db.cipher = cipher
	stats, err = db.AuditOutboxStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.Failed != 1 {
		t.Fatalf("failed outbox stats = %#v", stats)
	}
	var attempts int
	if err := db.pool.QueryRow(ctx, `SELECT attempts FROM audit_outbox WHERE source_id=$1`, retry.SourceID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("outbox attempts = %d, want 1", attempts)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE audit_outbox SET next_attempt_at=now() WHERE source_id=$1`, retry.SourceID); err != nil {
		t.Fatal(err)
	}
	if drained, err := db.ProcessAuditOutbox(ctx, 10); err != nil || drained != 1 {
		t.Fatalf("retry outbox = %d, %v", drained, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE source_id=$1`, retry.SourceID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("retried audit count = %d, want 1", auditCount)
	}
}

func updatedJobID(t *testing.T, db *Store, hostID string) string {
	t.Helper()
	var jobID string
	if err := db.pool.QueryRow(context.Background(), `SELECT id FROM jobs WHERE host_id=$1 AND status='queued' LIMIT 1`, hostID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	return jobID
}

func TestAuditChainCheckpointAndTamperDetection(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	for i := 0; i < 4; i++ {
		if err := db.AddAudit(ctx, Audit{
			ActorID: "user", ActorName: "tester", Action: fmt.Sprintf("test.action.%d", i),
			Detail: map[string]any{"index": i}, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("initial VerifyAuditChain: %v", err)
	}
	var hash string
	if err := db.pool.QueryRow(ctx, `SELECT entry_hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "hmac-sha256:") {
		t.Fatalf("new audit hash = %q, want HMAC", hash)
	}
	removed, err := db.PurgeOldAudit(ctx, base.Add(2*time.Minute+30*time.Second))
	if err != nil || removed != 3 {
		t.Fatalf("PurgeOldAudit = %d, %v", removed, err)
	}
	if err := db.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain after checkpoint: %v", err)
	}
	if err := db.AddAudit(ctx, Audit{ActorName: "tester", Action: "test.action.after_checkpoint"}); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain after appending: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE audit_logs SET detail='{"tampered":true}' WHERE id=(SELECT max(id) FROM audit_logs)`); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyAuditChain(ctx); err == nil {
		t.Fatal("tampered audit row passed chain verification")
	}
}

func TestPurgeOldAuditPreservesNewerRecordAfterLateOldEvent(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)

	// outbox replay is intentionally late: the newer record gets the lower
	// sequence id, while the replayed old event gets a higher one. Retention
	// must only remove the oldest contiguous prefix of the hash chain.
	for _, event := range []Audit{
		{ActorName: "tester", Action: "test.old.first", CreatedAt: base},
		{ActorName: "tester", Action: "test.new.retained", CreatedAt: base.Add(10 * time.Minute)},
		{ActorName: "tester", Action: "test.old.replayed", CreatedAt: base.Add(time.Minute)},
	} {
		if err := db.AddAudit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := db.PurgeOldAudit(ctx, base.Add(5*time.Minute))
	if err != nil || removed != 1 {
		t.Fatalf("PurgeOldAudit = %d, %v; want 1", removed, err)
	}
	var newer, replayed int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action='test.new.retained'),
		count(*) FILTER (WHERE action='test.old.replayed') FROM audit_logs`).Scan(&newer, &replayed); err != nil {
		t.Fatal(err)
	}
	if newer != 1 || replayed != 1 {
		t.Fatalf("retained records = newer:%d replayed:%d; want 1/1", newer, replayed)
	}
	if err := db.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain after prefix purge: %v", err)
	}
}

func TestUpgradeLegacyHostURIsBindsCiphertextToHost(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createLegacyHost := func() Host {
		hostID := id.New("host")
		legacy, err := db.cipher.Seal("qemu+ssh://root@example/system")
		if err != nil {
			t.Fatal(err)
		}
		host := Host{ID: hostID, Name: id.New("hostname"), URICiphertext: legacy, Enabled: true}
		if err := db.CreateHost(ctx, host); err != nil {
			t.Fatal(err)
		}
		return host
	}
	hostA := createLegacyHost()
	hostB := createLegacyHost()
	if _, err := db.cipher.OpenFor("host:"+hostA.ID, hostA.URICiphertext); err == nil {
		t.Fatal("normal purpose-bound read accepted v1 host ciphertext")
	}
	upgraded, err := db.UpgradeLegacyHostURIs(ctx)
	if err != nil || upgraded != 2 {
		t.Fatalf("UpgradeLegacyHostURIs = %d, %v", upgraded, err)
	}
	for _, want := range []Host{hostA, hostB} {
		host, err := db.GetHost(ctx, want.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(host.URICiphertext, "v2:") {
			t.Fatalf("host %s ciphertext was not upgraded", host.ID)
		}
		plain, err := db.cipher.OpenFor("host:"+host.ID, host.URICiphertext)
		if err != nil || plain != "qemu+ssh://root@example/system" {
			t.Fatalf("OpenFor(%s) = %q, %v", host.ID, plain, err)
		}
	}
	upgradedA, err := db.GetHost(ctx, hostA.ID)
	if err != nil {
		t.Fatal(err)
	}
	upgradedB, err := db.GetHost(ctx, hostB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE hosts SET uri_ciphertext=$2 WHERE id=$1`, hostA.ID, upgradedB.URICiphertext); err != nil {
		t.Fatal(err)
	}
	if _, err := db.cipher.OpenFor("host:"+hostA.ID, upgradedB.URICiphertext); err == nil {
		t.Fatal("host ciphertext replayed onto another host decrypted successfully")
	}
	if _, err := db.cipher.OpenFor("host:"+hostB.ID, upgradedA.URICiphertext); err == nil {
		t.Fatal("host ciphertext replayed onto the other host decrypted successfully")
	}
}

func TestUpgradeLegacyJobCiphertextsBindsJobAndField(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	ids := []string{id.New("job"), id.New("job")}
	args := []string{`["list","--all"]`, `["dominfo","web-prod"]`}
	for i, jobID := range ids {
		if err := db.EnqueueJob(ctx, Job{
			ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: "legacy", Tool: "virsh",
			ArgsJSON: args[i], TimeoutSeconds: 30,
		}); err != nil {
			t.Fatal(err)
		}
		legacyArgs, err := db.cipher.Seal(args[i])
		if err != nil {
			t.Fatal(err)
		}
		legacyOutput, err := db.cipher.Seal("output-" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `UPDATE jobs SET args_json='[]'::jsonb, stdin='', output='',
			args_ciphertext=$2, output_ciphertext=$3 WHERE id=$1`, jobID, legacyArgs, legacyOutput); err != nil {
			t.Fatal(err)
		}
		if _, err := db.cipher.OpenFor("job:"+jobID+":args", legacyArgs); err == nil {
			t.Fatal("normal purpose-bound read accepted v1 job ciphertext")
		}
	}
	upgraded, err := db.UpgradeLegacyJobCiphertexts(ctx, 10)
	if err != nil || upgraded != 2 {
		t.Fatalf("UpgradeLegacyJobCiphertexts = %d, %v", upgraded, err)
	}
	for i, jobID := range ids {
		job, err := db.GetJob(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.ArgsJSON != args[i] || job.Output != "output-"+jobID {
			t.Fatalf("migrated job %s = args %q output %q", jobID, job.ArgsJSON, job.Output)
		}
	}
	var argsA, argsB, outputB string
	if err := db.pool.QueryRow(ctx, `SELECT args_ciphertext FROM jobs WHERE id=$1`, ids[0]).Scan(&argsA); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT args_ciphertext, output_ciphertext FROM jobs WHERE id=$1`, ids[1]).Scan(&argsB, &outputB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET args_ciphertext=$2 WHERE id=$1`, ids[0], argsB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetJob(ctx, ids[0]); err == nil {
		t.Fatal("job arguments replayed onto another job decrypted successfully")
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET args_ciphertext=$2, output_ciphertext=$3 WHERE id=$1`, ids[0], argsA, outputB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetJob(ctx, ids[0]); err == nil {
		t.Fatal("job output replayed onto another job decrypted successfully")
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET args_ciphertext=$2, output_ciphertext=$3 WHERE id=$1`, ids[0], argsA, argsA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetJob(ctx, ids[0]); err == nil {
		t.Fatal("job arguments replayed into another field decrypted successfully")
	}
}

func TestEncryptLegacyJobsClearsResidualPlaintext(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	jobID := id.New("job")
	originalArgs := `["list","--all"]`
	if err := db.EnqueueJob(ctx, Job{
		ID: jobID, UserID: user.ID, HostID: host.ID, Kind: "command", Label: "legacy", Tool: "virsh",
		ArgsJSON: originalArgs, Stdin: "original stdin", TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimJob(ctx, "worker")
	if err != nil || job == nil {
		t.Fatalf("ClaimJob = %#v, %v", job, err)
	}
	if finished, err := db.FinishJob(ctx, jobID, "worker", "succeeded", "real output", "real error"); err != nil || !finished {
		t.Fatalf("FinishJob = %v, %v", finished, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE jobs SET args_json='["leaked"]'::jsonb, stdin='leaked',
		output='leaked', error='leaked' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	cleaned, err := db.EncryptLegacyJobs(ctx, 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("EncryptLegacyJobs = %d, %v", cleaned, err)
	}
	var argsPlain, stdinPlain, outputPlain, errorPlain string
	if err := db.pool.QueryRow(ctx, `SELECT args_json::text, stdin, output, error FROM jobs WHERE id=$1`, jobID).
		Scan(&argsPlain, &stdinPlain, &outputPlain, &errorPlain); err != nil {
		t.Fatal(err)
	}
	if argsPlain != "[]" || stdinPlain != "" || outputPlain != "" || errorPlain != "" {
		t.Fatalf("residual plaintext = args %q stdin %q output %q error %q", argsPlain, stdinPlain, outputPlain, errorPlain)
	}
	stored, err := db.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ArgsJSON != originalArgs || stored.Stdin != "original stdin" || stored.Output != "real output" || stored.Error != "real error" {
		t.Fatalf("encrypted values changed during cleanup: %#v", stored)
	}
}

func TestEnqueueJobRejectsUnknownKind(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user := createTestUser(t, db, "admin")
	host := createTestHost(t, db)
	err := db.EnqueueJob(ctx, Job{
		ID: id.New("job"), UserID: user.ID, HostID: host.ID, Kind: "unknown", Tool: "virsh",
		ArgsJSON: `["version"]`, TimeoutSeconds: 30,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid job kind") {
		t.Fatalf("EnqueueJob unknown kind = %v", err)
	}
}

func TestValidJobKindIncludesEveryQueuedOperation(t *testing.T) {
	kinds := []string{
		"command", "vm-create", "vm-clone", "vm-action", "host-test", "filesystem-inspect",
		"storage-action", "network-action", "disk-action", "vm-resource",
	}
	for _, kind := range kinds {
		if !validJobKind(kind) {
			t.Errorf("validJobKind(%q) = false", kind)
		}
	}
	if validJobKind("unknown") {
		t.Fatal("unknown job kind was accepted")
	}
}

func TestAuditChainRejectsLegacyDowngrade(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.AddAudit(ctx, Audit{ActorName: "tester", Action: "audit.signed"}); err != nil {
		t.Fatal(err)
	}
	var previous string
	if err := db.pool.QueryRow(ctx, `SELECT entry_hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	forged := Audit{ActorName: "attacker", Action: "audit.downgrade", CreatedAt: createdAt}
	legacyHash := auditHashLegacy(previous, forged, "{}", createdAt)
	if _, err := db.pool.Exec(ctx, `INSERT INTO audit_logs(actor_name,action,detail,created_at,prev_hash,entry_hash)
		VALUES($1,$2,'{}'::jsonb,$3,$4,$5)`, forged.ActorName, forged.Action, createdAt, previous, legacyHash); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyAuditChain(ctx); err == nil || !strings.Contains(err.Error(), "downgraded") {
		t.Fatalf("VerifyAuditChain with appended legacy row = %v", err)
	}
}

func TestMigrationChecksumAndUnknownVersionRejected(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.pool.Exec(ctx, `UPDATE schema_migrations SET checksum='changed' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("Migrate with changed checksum = %v", err)
	}
}

func TestMigrateRejectsSchemaNewerThanBinary(t *testing.T) {
	db := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.pool.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES(999,'future')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "not present in this binary") {
		t.Fatalf("Migrate with future schema = %v", err)
	}
}
