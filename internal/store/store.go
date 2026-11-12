package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/secret"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	pool                 *pgxpool.Pool
	cipher               *secret.Cipher
	maxActiveJobsPerUser int
	maxActiveJobs        int
}

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Session struct {
	TokenHash string
	CSRFToken string
	ExpiresAt time.Time
	LastSeen  time.Time
	User      User
}

type Host struct {
	ID            string
	Name          string
	URICiphertext string
	Description   string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Job struct {
	ID       string
	UserID   string
	UserName string
	HostID   string
	HostName string
	// ExpectedHostURICiphertext is an enqueue-time optimistic concurrency
	// check for host-bound jobs. The submitter records the host ciphertext it
	// validated; EnqueueJob refuses to persist the job if an administrator
	// changed the connection URI in the meantime. It is not persisted: the
	// host row lock taken by EnqueueJob makes URI replacement impossible while
	// the job is queued or running.
	ExpectedHostURICiphertext string
	Kind                      string
	Label                     string
	Tool                      string
	ArgsJSON                  string
	Stdin                     string
	ReadOnly                  bool
	TimeoutSeconds            int
	Status                    string
	Output                    string
	Error                     string
	CancelRequested           bool
	Sensitive                 bool
	DedupeKey                 string
	ResourceKey               string
	CreatedAt                 time.Time
	StartedAt                 time.Time
	FinishedAt                time.Time

	argsCiphertext   string
	stdinCiphertext  string
	outputCiphertext string
	errorCiphertext  string
}

// DuplicateJobError means an equivalent job is already queued or running.
type DuplicateJobError struct{ ExistingID string }

func (e *DuplicateJobError) Error() string { return "an equivalent job is already active" }

var (
	ErrHostHasActiveJobs = errors.New("host has active jobs")
	ErrHostChanged       = errors.New("host connection changed while enqueueing job")
	ErrHostRequired      = errors.New("job requires a host")
	ErrJobQuotaExceeded  = errors.New("job queue quota exceeded")
	ErrLastAdmin         = errors.New("不能降级或禁用最后一个管理员")
	// ErrSessionCredentialsChanged means the password changed (or the account
	// was disabled) after it was verified but before the session was persisted.
	ErrSessionCredentialsChanged = errors.New("user credentials changed before session creation")
	ErrAuditDuplicate            = errors.New("audit entry already applied")
)

const (
	userAdminLock int64 = 0x57564355534552
	auditLock     int64 = 0x5756434155444954
	jobQueueLock  int64 = 0x5756434A4F425155
)

// SystemUserIDPrefix reserves database-backed principal IDs used by bearer-token
// API automation. These accounts cannot sign in and are hidden from user
// management, but their IDs satisfy the jobs.user_id foreign key and make job
// ownership/auditing deterministic across replicas and restarts.
const SystemUserIDPrefix = "system:"

func IsSystemUserID(id string) bool { return strings.HasPrefix(id, SystemUserIDPrefix) }

const (
	defaultMaxActiveJobsPerUser = 20
	defaultMaxActiveJobs        = 200
)

type Audit struct {
	ActorID   string
	ActorName string
	Action    string
	HostID    string
	VMName    string
	Detail    map[string]any
	RemoteIP  string
	CreatedAt time.Time
	SourceID  string
}

type QueueStats struct {
	Queued    int
	Running   int
	Succeeded int
	Failed    int
	Cancelled int
}

type AuditOutboxStats struct {
	Total  int
	Due    int
	Failed int
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool, maxActiveJobsPerUser: defaultMaxActiveJobsPerUser, maxActiveJobs: defaultMaxActiveJobs}, nil
}

// SetJobLimits changes the queued+running job ceilings for this process.
// Limits are also enforced under a database advisory lock, so multiple
// application replicas observe the same quota.
func (s *Store) SetJobLimits(perUser, total int) {
	if perUser > 0 {
		s.maxActiveJobsPerUser = perUser
	}
	if total > 0 {
		s.maxActiveJobs = total
	}
}

func (s *Store) Close() { s.pool.Close() }

// SetCipher enables encryption of job arguments, stdin and results at rest.
// It is set once during application startup before the web server or workers
// begin using the store.
func (s *Store) SetCipher(c *secret.Cipher) { s.cipher = c }

// UpgradeLegacyHostURIs rewrites v1 host ciphertext with host-bound AAD. It is
// safe to run on every startup and prevents a database attacker from swapping
// one host's legacy ciphertext onto another host.
func (s *Store) UpgradeLegacyHostURIs(ctx context.Context) (int, error) {
	if s.cipher == nil {
		return 0, errors.New("job encryption key is not configured")
	}
	hosts, err := s.ListHosts(ctx)
	if err != nil {
		return 0, err
	}
	upgraded := 0
	for _, host := range hosts {
		if host.URICiphertext == "" {
			continue
		}
		purpose := "host:" + host.ID
		if strings.HasPrefix(host.URICiphertext, "v2:") {
			if _, err := s.cipher.OpenFor(purpose, host.URICiphertext); err != nil {
				return upgraded, fmt.Errorf("validate host URI %s: %w", host.ID, err)
			}
			continue
		}
		if !strings.HasPrefix(host.URICiphertext, "v1:") {
			return upgraded, fmt.Errorf("host URI %s uses an unsupported ciphertext version", host.ID)
		}
		plain, err := s.cipher.OpenLegacy(host.URICiphertext)
		if err != nil {
			return upgraded, fmt.Errorf("decrypt legacy host URI %s: %w", host.ID, err)
		}
		sealed, err := s.cipher.SealFor(purpose, plain)
		if err != nil {
			return upgraded, err
		}
		tag, err := s.pool.Exec(ctx, `UPDATE hosts SET uri_ciphertext=$2, updated_at=now() WHERE id=$1 AND uri_ciphertext=$3`, host.ID, sealed, host.URICiphertext)
		if err != nil {
			return upgraded, err
		}
		if tag.RowsAffected() == 1 {
			upgraded++
		}
	}
	return upgraded, nil
}

// EncryptLegacyJobs encrypts plaintext arguments, stdin and results left by
// older releases. The operation is batched so upgrades with large histories do
// not hold one enormous transaction.
func (s *Store) EncryptLegacyJobs(ctx context.Context, batchSize int) (int64, error) {
	if s.cipher == nil {
		return 0, errors.New("job encryption key is not configured")
	}
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 200
	}
	var total int64
	for {
		encrypted, done, err := s.encryptLegacyJobBatch(ctx, batchSize)
		total += encrypted
		if err != nil {
			return total, err
		}
		if done {
			return total, nil
		}
	}
}

// UpgradeLegacyJobCiphertexts rewrites pre-AAD job ciphertext with a purpose
// bound to the job ID and field. Normal reads reject v1 ciphertext, so a
// database attacker cannot replay an old value into another job after startup.
func (s *Store) UpgradeLegacyJobCiphertexts(ctx context.Context, batchSize int) (int64, error) {
	if s.cipher == nil {
		return 0, errors.New("job encryption key is not configured")
	}
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 200
	}
	var total int64
	for {
		upgraded, done, err := s.upgradeLegacyJobCiphertextBatch(ctx, batchSize)
		total += upgraded
		if err != nil {
			return total, err
		}
		if done {
			return total, nil
		}
	}
}

func (s *Store) upgradeLegacyJobCiphertextBatch(ctx context.Context, batchSize int) (int64, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT id, args_json::text, stdin, output, error,
		args_ciphertext, stdin_ciphertext, output_ciphertext, error_ciphertext
		FROM jobs
		WHERE args_ciphertext LIKE 'v1:%'
		   OR stdin_ciphertext LIKE 'v1:%'
		   OR output_ciphertext LIKE 'v1:%'
		   OR error_ciphertext LIKE 'v1:%'
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $1`, batchSize)
	if err != nil {
		return 0, false, err
	}
	type legacyJob struct {
		id, args, stdin, output, errText string
		argsCipher, stdinCipher          string
		outputCipher, errorCipher        string
	}
	batch := make([]legacyJob, 0, batchSize)
	for rows.Next() {
		var job legacyJob
		if err := rows.Scan(&job.id, &job.args, &job.stdin, &job.output, &job.errText,
			&job.argsCipher, &job.stdinCipher, &job.outputCipher, &job.errorCipher); err != nil {
			rows.Close()
			return 0, false, err
		}
		batch = append(batch, job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if len(batch) == 0 {
		return 0, true, tx.Commit(ctx)
	}

	var upgraded int64
	for _, job := range batch {
		reseal := func(field, ciphertext string) (string, error) {
			if ciphertext == "" {
				return "", nil
			}
			purpose := "job:" + job.id + ":" + field
			switch {
			case strings.HasPrefix(ciphertext, "v1:"):
				plain, err := s.cipher.OpenLegacy(ciphertext)
				if err != nil {
					return "", err
				}
				return s.cipher.SealFor(purpose, plain)
			case strings.HasPrefix(ciphertext, "v2:"):
				if _, err := s.cipher.OpenFor(purpose, ciphertext); err != nil {
					return "", err
				}
				return ciphertext, nil
			default:
				return "", errors.New("job field uses an unsupported ciphertext version")
			}
		}
		argsCipher, err := reseal("args", job.argsCipher)
		if err != nil {
			return upgraded, false, fmt.Errorf("upgrade job %s arguments: %w", job.id, err)
		}
		stdinCipher, err := reseal("stdin", job.stdinCipher)
		if err != nil {
			return upgraded, false, fmt.Errorf("upgrade job %s stdin: %w", job.id, err)
		}
		outputCipher, err := reseal("output", job.outputCipher)
		if err != nil {
			return upgraded, false, fmt.Errorf("upgrade job %s output: %w", job.id, err)
		}
		errorCipher, err := reseal("error", job.errorCipher)
		if err != nil {
			return upgraded, false, fmt.Errorf("upgrade job %s error: %w", job.id, err)
		}
		args, stdin, output, errText := job.args, job.stdin, job.output, job.errText
		if argsCipher != "" {
			args = "[]"
		}
		if stdinCipher != "" {
			stdin = ""
		}
		if outputCipher != "" {
			output = ""
		}
		if errorCipher != "" {
			errText = ""
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET args_json=$2::jsonb, stdin=$3, output=$4, error=$5,
			args_ciphertext=$6, stdin_ciphertext=$7, output_ciphertext=$8, error_ciphertext=$9 WHERE id=$1`,
			job.id, args, stdin, output, errText, argsCipher, stdinCipher, outputCipher, errorCipher); err != nil {
			return upgraded, false, err
		}
		upgraded++
	}
	if err := tx.Commit(ctx); err != nil {
		return upgraded, false, err
	}
	return upgraded, len(batch) < batchSize, nil
}

func (s *Store) encryptLegacyJobBatch(ctx context.Context, batchSize int) (int64, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT id, args_json::text, stdin, output, error,
		args_ciphertext, stdin_ciphertext, output_ciphertext, error_ciphertext
		FROM jobs
		WHERE (args_ciphertext='' AND args_json<>'[]'::jsonb)
		   OR (args_ciphertext<>'' AND args_json<>'[]'::jsonb)
		   OR (stdin_ciphertext='' AND stdin<>'')
		   OR (stdin_ciphertext<>'' AND stdin<>'')
		   OR (output_ciphertext='' AND output<>'')
		   OR (output_ciphertext<>'' AND output<>'')
		   OR (error_ciphertext='' AND error<>'')
		   OR (error_ciphertext<>'' AND error<>'')
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $1`, batchSize)
	if err != nil {
		return 0, false, err
	}
	type legacyJob struct {
		id, args, stdin, output, errText      string
		argsCipher, stdinCipher, outputCipher string
		errorCipher                           string
	}
	batch := make([]legacyJob, 0, batchSize)
	for rows.Next() {
		var job legacyJob
		if err := rows.Scan(&job.id, &job.args, &job.stdin, &job.output, &job.errText,
			&job.argsCipher, &job.stdinCipher, &job.outputCipher, &job.errorCipher); err != nil {
			rows.Close()
			return 0, false, err
		}
		batch = append(batch, job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if len(batch) == 0 {
		return 0, true, tx.Commit(ctx)
	}

	var encrypted int64
	for _, job := range batch {
		seal := func(field, value string) (string, error) {
			if value == "" {
				return "", nil
			}
			return s.cipher.SealFor("job:"+job.id+":"+field, value)
		}
		argsCipher, stdinCipher, outputCipher, errorCipher := job.argsCipher, job.stdinCipher, job.outputCipher, job.errorCipher
		if argsCipher == "" {
			if job.args != "[]" {
				if argsCipher, err = seal("args", job.args); err != nil {
					return encrypted, false, err
				}
			}
		} else if job.args != "[]" {
			job.args = "[]"
		}
		if stdinCipher == "" {
			if job.stdin != "" {
				if stdinCipher, err = seal("stdin", job.stdin); err != nil {
					return encrypted, false, err
				}
			}
		} else if job.stdin != "" {
			job.stdin = ""
		}
		if outputCipher == "" {
			if job.output != "" {
				if outputCipher, err = seal("output", job.output); err != nil {
					return encrypted, false, err
				}
			}
		} else if job.output != "" {
			job.output = ""
		}
		if errorCipher == "" {
			if job.errText != "" {
				if errorCipher, err = seal("error", job.errText); err != nil {
					return encrypted, false, err
				}
			}
		} else if job.errText != "" {
			job.errText = ""
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET args_json=$2::jsonb, stdin=$3, output=$4, error=$5,
			args_ciphertext=$6, stdin_ciphertext=$7, output_ciphertext=$8, error_ciphertext=$9 WHERE id=$1`,
			job.id, job.args, job.stdin, job.output, job.errText, argsCipher, stdinCipher, outputCipher, errorCipher); err != nil {
			return encrypted, false, err
		}
		encrypted++
	}
	if err := tx.Commit(ctx); err != nil {
		return encrypted, false, err
	}
	return encrypted, len(batch) < batchSize, nil
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	const migrationLock int64 = 0x5756434d494752
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLock)
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := conn.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add schema migration checksum: %w", err)
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	type migrationFile struct {
		version  int
		name     string
		sql      []byte
		checksum string
	}
	ordered := make([]migrationFile, 0, len(files))
	seenVersions := make(map[int]string, len(files))
	for _, name := range files {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil || version <= 0 {
			return fmt.Errorf("invalid migration name %q", name)
		}
		if previous, exists := seenVersions[version]; exists {
			return fmt.Errorf("duplicate migration version %d: %s and %s", version, previous, name)
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(sql)
		ordered = append(ordered, migrationFile{
			version: version, name: name, sql: sql, checksum: fmt.Sprintf("%x", sum[:]),
		})
		seenVersions[version] = name
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].version == ordered[j].version {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].version < ordered[j].version
	})

	applied := make(map[int]string)
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for version := range applied {
		if _, exists := seenVersions[version]; !exists {
			return fmt.Errorf("database contains migration %d which is not present in this binary; refusing to run an older schema", version)
		}
	}

	highestApplied := 0
	for version := range applied {
		if version > highestApplied {
			highestApplied = version
		}
	}
	for _, migration := range ordered {
		appliedChecksum, exists := applied[migration.version]
		if exists {
			if appliedChecksum != "" && appliedChecksum != migration.checksum {
				return fmt.Errorf("migration %s checksum changed after it was applied", migration.name)
			}
			if appliedChecksum == "" {
				if _, err := conn.Exec(ctx, `UPDATE schema_migrations SET checksum=$2 WHERE version=$1 AND checksum=''`, migration.version, migration.checksum); err != nil {
					return fmt.Errorf("backfill migration %s checksum: %w", migration.name, err)
				}
			}
			continue
		}
		if migration.version < highestApplied {
			return fmt.Errorf("migration %s is missing but newer migrations are already applied", migration.name)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(migration.sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", migration.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, checksum) VALUES($1,$2)`, migration.version, migration.checksum); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		highestApplied = migration.version
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id NOT LIKE 'system:%'`).Scan(&n)
	return n, err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=false`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u User) error {
	if !validUserRole(u.Role) {
		return fmt.Errorf("invalid user role %q", u.Role)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id, username, password_hash, role, disabled)
		VALUES($1,$2,$3,$4,$5)`, u.ID, u.Username, u.PasswordHash, u.Role, u.Disabled)
	return err
}

// EnsureSystemUser creates or refreshes a disabled principal for a configured
// bearer token. The account has no usable login path, is excluded from user
// management, and only provides a durable user_id for jobs and audit records.
func (s *Store) EnsureSystemUser(ctx context.Context, u User) error {
	if !IsSystemUserID(u.ID) {
		return fmt.Errorf("system user ID must use the %q prefix", SystemUserIDPrefix)
	}
	if !validUserRole(u.Role) {
		return fmt.Errorf("invalid user role %q", u.Role)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id, username, password_hash, role, disabled)
		VALUES($1,$2,$3,$4,TRUE)
		ON CONFLICT (id) DO UPDATE SET username=EXCLUDED.username, password_hash=EXCLUDED.password_hash,
			role=EXCLUDED.role, disabled=TRUE, updated_at=now()`, u.ID, u.Username, u.PasswordHash, u.Role)
	return err
}

// EnsureInitialAdmin creates the first administrator exactly once even when
// multiple replicas start at the same time. The second replica returns false
// and must not print or persist its locally generated password.
func (s *Store) EnsureInitialAdmin(ctx context.Context, u User) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const bootstrapLock int64 = 0x575643424f4f54
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLock); err != nil {
		return false, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id NOT LIKE 'system:%'`).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(id, username, password_hash, role, disabled)
		VALUES($1,$2,$3,$4,$5)`, u.ID, u.Username, u.PasswordHash, u.Role, u.Disabled); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `SELECT id, username, password_hash, role, disabled, created_at, updated_at
		FROM users WHERE username=$1`, username))
}

func (s *Store) GetUserByID(ctx context.Context, id string) (User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `SELECT id, username, password_hash, role, disabled, created_at, updated_at
		FROM users WHERE id=$1`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanUser(row rowScanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return s.ListUsersPage(ctx, 0, 500)
}

func (s *Store) ListUsersPage(ctx context.Context, offset, limit int) ([]User, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `SELECT id, username, '' AS password_hash, role, disabled, created_at, updated_at
		FROM users WHERE id NOT LIKE 'system:%' ORDER BY username LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) UserCanAccessVM(ctx context.Context, userID, hostID, vmName string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM user_vm_access WHERE user_id=$1 AND host_id=$2 AND vm_name=$3
	)`, userID, hostID, vmName).Scan(&exists)
	return exists, err
}

func (s *Store) GrantVMAccess(ctx context.Context, userID, hostID, vmName string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_vm_access (user_id, host_id, vm_name)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, userID, hostID, vmName)
	return err
}

func (s *Store) RevokeVMAccess(ctx context.Context, userID, hostID, vmName string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_vm_access
		WHERE user_id=$1 AND host_id=$2 AND vm_name=$3`, userID, hostID, vmName)
	return err
}

func (s *Store) ListVMAccess(ctx context.Context, userID string) ([]VMAccess, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.host_id, h.name, a.vm_name
		FROM user_vm_access a JOIN hosts h ON h.id=a.host_id
		WHERE a.user_id=$1 ORDER BY h.name, a.vm_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var access []VMAccess
	for rows.Next() {
		var item VMAccess
		if err := rows.Scan(&item.HostID, &item.HostName, &item.VMName); err != nil {
			return nil, err
		}
		access = append(access, item)
	}
	return access, rows.Err()
}

type VMAccess struct {
	HostID   string
	HostName string
	VMName   string
}

func (s *Store) UpdateUserRole(ctx context.Context, id, role string) error {
	if !validUserRole(role) {
		return fmt.Errorf("invalid user role %q", role)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminLock); err != nil {
		return err
	}
	var oldRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&oldRole); err != nil {
		return err
	}
	if oldRole == "admin" && role != "admin" {
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=false AND id<>$1`, id).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role=$2, updated_at=now() WHERE id=$1`, id, role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminLock); err != nil {
		return err
	}
	if disabled {
		var role string
		if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&role); err != nil {
			return err
		}
		if role == "admin" {
			var admins int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=false AND id<>$1`, id).Scan(&admins); err != nil {
				return err
			}
			if admins == 0 {
				return ErrLastAdmin
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET disabled=$2, updated_at=now() WHERE id=$1`, id, disabled); err != nil {
		return err
	}
	if disabled {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) SetPassword(ctx context.Context, id, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, hash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateSession(ctx context.Context, tokenHash, userID, passwordHash, csrf string, expires time.Time, ip, ua string, maxPerUser int) error {
	if maxPerUser < 1 {
		return errors.New("session limit must be positive")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize session creation for this user and reject accounts whose
	// password or enabled state changed after credential verification. Without
	// the password-hash check, a password reset racing with an in-flight login
	// could leave a fresh session authenticated by the old password.
	var lockedID string
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND password_hash=$2 AND disabled=false FOR UPDATE`,
		userID, passwordHash).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionCredentialsChanged
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND expires_at <= now()`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,remote_ip,user_agent)
		VALUES($1,$2,$3,$4,$5,$6)`, tokenHash, userID, csrf, expires, ip, ua); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash NOT IN (
		SELECT token_hash FROM sessions WHERE user_id=$1 AND token_hash<>$3
		ORDER BY last_seen DESC, created_at DESC, token_hash DESC LIMIT $2
	) AND token_hash<>$3`, userID, maxPerUser-1, tokenHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx, `SELECT s.token_hash,s.csrf_token,s.expires_at,s.last_seen,
		u.id,u.username,u.password_hash,u.role,u.disabled,u.created_at,u.updated_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at > now() AND u.disabled=false`, tokenHash).
		Scan(&sess.TokenHash, &sess.CSRFToken, &sess.ExpiresAt, &sess.LastSeen,
			&sess.User.ID, &sess.User.Username, &sess.User.PasswordHash, &sess.User.Role,
			&sess.User.Disabled, &sess.User.CreatedAt, &sess.User.UpdatedAt)
	if err == nil {
		// The password hash is only needed during credential verification and
		// must never travel with an authenticated request context.
		sess.User.PasswordHash = ""
	}
	return sess, err
}

func (s *Store) TouchSession(ctx context.Context, tokenHash, ip, ua string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen=now(), remote_ip=$2, user_agent=$3
		WHERE token_hash=$1`, tokenHash, ip, ua)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClaimConsoleToken atomically records a console token use. It returns false
// when the token nonce/digest was already consumed. The unique constraint is
// in PostgreSQL so replay protection also works across application replicas.
func (s *Store) ClaimConsoleToken(ctx context.Context, tokenHash string, expiresAt time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO console_token_uses(token_hash,expires_at)
		VALUES($1,$2) ON CONFLICT (token_hash) DO NOTHING`, tokenHash, expiresAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) PurgeExpiredConsoleTokens(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM console_token_uses WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ReserveLoginAttempt atomically checks all login limits and records one
// in-flight attempt before password verification starts. A successful login
// must call CompleteLoginAttempt to clear the per-pair counters and refund the
// IP/global slot. Serializing the check and increment prevents a burst of
// concurrent requests from all passing the limit before any failure is stored.
func (s *Store) ReserveLoginAttempt(ctx context.Context, keys []string, maxFailures, ipMaxFailures, globalMaxFailures int, window time.Duration) (bool, error) {
	if len(keys) == 0 {
		return true, nil
	}
	ordered := append([]string(nil), keys...)
	sort.Strings(ordered)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	interval := fmt.Sprintf("%d seconds", int(window.Seconds()))
	for _, key := range ordered {
		if _, err := tx.Exec(ctx, `INSERT INTO login_attempts(key,failures,first_at,last_at)
			VALUES($1,0,now(),now())
			-- A concurrent CompleteLoginAttempt may delete a conflicting row.
			-- DO NOTHING can then leave no row at all, so force an upsert and
			-- keep the row locked until this reservation commits.
			ON CONFLICT(key) DO UPDATE SET key=EXCLUDED.key`, key); err != nil {
			return false, err
		}
	}
	type attemptState struct {
		key      string
		failures int
		expired  bool
	}
	rows, err := tx.Query(ctx, `SELECT key,failures,last_at <= now()-$2::interval
		FROM login_attempts WHERE key=ANY($1) ORDER BY key FOR UPDATE`, ordered, interval)
	if err != nil {
		return false, err
	}
	states := make([]attemptState, 0, len(ordered))
	for rows.Next() {
		var state attemptState
		if err := rows.Scan(&state.key, &state.failures, &state.expired); err != nil {
			rows.Close()
			return false, err
		}
		states = append(states, state)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(states) != len(ordered) {
		return false, errors.New("failed to lock all login attempt counters")
	}
	for _, state := range states {
		limit := maxFailures
		if strings.HasPrefix(state.key, "global:") {
			// This counter is deliberately not a hard stop. It is still
			// incremented and refunded for observability, but a globally
			// exhausted counter must never deny every legitimate account.
			continue
		} else if strings.HasPrefix(state.key, "ip:") {
			limit = ipMaxFailures
		}
		if limit < 1 {
			limit = 1
		}
		failures := state.failures
		if state.expired {
			failures = 0
		}
		if failures >= limit {
			return false, nil
		}
	}
	for _, state := range states {
		if state.expired {
			_, err = tx.Exec(ctx, `UPDATE login_attempts SET failures=1, first_at=now(), last_at=now() WHERE key=$1`, state.key)
		} else {
			_, err = tx.Exec(ctx, `UPDATE login_attempts SET failures=failures+1, last_at=now() WHERE key=$1`, state.key)
		}
		if err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// CompleteLoginAttempt clears the per-pair counters and refunds the
// IP/global attempt reserved by ReserveLoginAttempt. Other failures from the
// same IP and the global emergency counter are intentionally preserved.
func (s *Store) CompleteLoginAttempt(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	clearKeys := make([]string, 0, len(keys))
	refundKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.HasPrefix(key, "ip:") || strings.HasPrefix(key, "global:") {
			refundKeys = append(refundKeys, key)
		} else {
			clearKeys = append(clearKeys, key)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// ReserveLoginAttempt locks all counters in sorted key order. Take the
	// same locks here before the DELETE/UPDATE statements, otherwise a refund
	// can hold pair/user rows while waiting for ip/global rows and deadlock
	// with a concurrent reservation moving in the opposite direction.
	if err := lockLoginAttemptKeys(ctx, tx, keys); err != nil {
		return err
	}
	if len(clearKeys) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM login_attempts WHERE key=ANY($1)`, clearKeys); err != nil {
			return err
		}
	}
	if len(refundKeys) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE login_attempts SET failures=GREATEST(failures-1,0) WHERE key=ANY($1)`, refundKeys); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM login_attempts WHERE key=ANY($1) AND failures<=0`, refundKeys); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func lockLoginAttemptKeys(ctx context.Context, tx pgx.Tx, keys []string) error {
	ordered := append([]string(nil), keys...)
	sort.Strings(ordered)
	rows, err := tx.Query(ctx, `SELECT key FROM login_attempts WHERE key=ANY($1) ORDER BY key FOR UPDATE`, ordered)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) PurgeLoginAttempts(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM login_attempts WHERE last_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) CreateHost(ctx context.Context, h Host) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO hosts(id,name,uri_ciphertext,description,enabled)
		VALUES($1,$2,$3,$4,$5)`, h.ID, h.Name, h.URICiphertext, h.Description, h.Enabled)
	return err
}

func (s *Store) UpdateHost(ctx context.Context, h Host) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentURI string
	if err := tx.QueryRow(ctx, `SELECT uri_ciphertext FROM hosts WHERE id=$1 FOR UPDATE`, h.ID).Scan(&currentURI); err != nil {
		return err
	}
	if currentURI != h.URICiphertext {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE host_id=$1 AND status IN ('queued','running'))`, h.ID).Scan(&active); err != nil {
			return err
		}
		if active {
			// A queued/running job resolves its connection URI from the host
			// row at execution time. Changing that URI underneath it could run
			// an operation against a different hypervisor than the submitter saw.
			return ErrHostHasActiveJobs
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE hosts SET name=$2,uri_ciphertext=$3,description=$4,enabled=$5,updated_at=now()
		WHERE id=$1`, h.ID, h.Name, h.URICiphertext, h.Description, h.Enabled); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListHosts(ctx context.Context) ([]Host, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,uri_ciphertext,description,enabled,created_at,updated_at
		FROM hosts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

func (s *Store) GetHost(ctx context.Context, id string) (Host, error) {
	return scanHost(s.pool.QueryRow(ctx, `SELECT id,name,uri_ciphertext,description,enabled,created_at,updated_at
		FROM hosts WHERE id=$1`, id))
}

func scanHost(row rowScanner) (Host, error) {
	var h Host
	err := row.Scan(&h.ID, &h.Name, &h.URICiphertext, &h.Description, &h.Enabled, &h.CreatedAt, &h.UpdatedAt)
	return h, err
}

func (s *Store) DeleteHost(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT id FROM hosts WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE host_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrHostHasActiveJobs
	}
	if _, err := tx.Exec(ctx, `DELETE FROM hosts WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) EnqueueJob(ctx context.Context, j Job) error {
	if s.cipher == nil {
		return errors.New("job encryption key is not configured")
	}
	if !validJobKind(j.Kind) {
		return fmt.Errorf("invalid job kind %q", j.Kind)
	}
	if strings.TrimSpace(j.HostID) == "" {
		return ErrHostRequired
	}
	// Serialization is a store invariant, not a web-handler convention.
	// Direct callers (tests, future APIs and maintenance tools) must not be
	// able to bypass host-level serialization by supplying an arbitrary key.
	if j.ReadOnly {
		j.ResourceKey = ""
	} else {
		j.ResourceKey = "host:" + j.HostID
	}
	if j.DedupeKey == "" {
		j.DedupeKey = jobDedupeKey(s.cipher.DerivedKey("job-dedupe"), j.Kind, j.UserID, j.HostID, j.Tool, j.ArgsJSON, j.Stdin)
	}
	// An INSERT can lose the unique-dedupe race to a job that finishes before
	// the follow-up lookup. Retry that narrow window instead of surfacing a
	// spurious uniqueness error to the caller.
	for attempt := 0; attempt < 3; attempt++ {
		err := s.enqueueJobOnce(ctx, j)
		if err == nil {
			return nil
		}
		var duplicate *DuplicateJobError
		if errors.As(err, &duplicate) {
			return duplicate
		}
		if errors.Is(err, errJobDedupeRace) {
			continue
		}
		return err
	}
	return errors.New("job dedupe state kept changing during enqueue; please retry")
}

var errJobDedupeRace = errors.New("job dedupe state changed during enqueue")

func (s *Store) enqueueJobOnce(ctx context.Context, j Job) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var enabled bool
	var currentURI string
	// Lock the parent row so an enqueue racing with host deletion either
	// completes first (and blocks deletion) or observes the deleted row.
	if err := tx.QueryRow(ctx, `SELECT enabled,uri_ciphertext FROM hosts WHERE id=$1 FOR SHARE`, j.HostID).Scan(&enabled, &currentURI); err != nil {
		return err
	}
	if !enabled {
		return errors.New("主机已禁用")
	}
	if j.ExpectedHostURICiphertext != "" && currentURI != j.ExpectedHostURICiphertext {
		return ErrHostChanged
	}

	// Quota checks and dedupe resolution must see a stable snapshot when many
	// requests arrive together. The global lock is short-lived and only covers
	// enqueue bookkeeping; command execution never holds it.
	if s.maxActiveJobs > 0 || s.maxActiveJobsPerUser > 0 {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobQueueLock); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "job-user:"+j.UserID); err != nil {
			return err
		}
	}

	if j.DedupeKey != "" {
		var existing string
		err := tx.QueryRow(ctx, `SELECT id FROM jobs WHERE dedupe_key=$1 AND status IN ('queued','running') LIMIT 1`, j.DedupeKey).Scan(&existing)
		if err == nil {
			return &DuplicateJobError{ExistingID: existing}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	if err := s.checkJobQuota(ctx, tx, j.UserID); err != nil {
		return err
	}

	argsJSON, stdin := j.ArgsJSON, j.Stdin
	argsCipher, stdinCipher := "", ""
	if s.cipher != nil {
		if argsJSON != "" {
			argsCipher, err = s.cipher.SealFor("job:"+j.ID+":args", argsJSON)
			if err != nil {
				return err
			}
		}
		if stdin != "" {
			stdinCipher, err = s.cipher.SealFor("job:"+j.ID+":stdin", stdin)
			if err != nil {
				return err
			}
		}
		argsJSON, stdin = "[]", ""
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(id,user_id,host_id,kind,label,tool,args_json,stdin,read_only,timeout_seconds,status,sensitive,args_ciphertext,stdin_ciphertext,dedupe_key,resource_key)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,'queued',$11,$12,$13,$14,$15)`,
		j.ID, j.UserID, j.HostID, j.Kind, j.Label, j.Tool, argsJSON, stdin, j.ReadOnly, j.TimeoutSeconds,
		j.Sensitive, argsCipher, stdinCipher, j.DedupeKey, j.ResourceKey)
	if err != nil {
		if IsUniqueViolation(err) && j.DedupeKey != "" {
			// The failed INSERT aborted this transaction. Roll it back before
			// resolving the active job through a fresh snapshot; the competing
			// insert either committed (and is now visible) or was rolled back.
			_ = tx.Rollback(ctx)
			var existing string
			queryErr := s.pool.QueryRow(ctx, `SELECT id FROM jobs WHERE dedupe_key=$1 AND status IN ('queued','running') ORDER BY created_at, id LIMIT 1`, j.DedupeKey).Scan(&existing)
			if queryErr == nil {
				return &DuplicateJobError{ExistingID: existing}
			}
			if !errors.Is(queryErr, pgx.ErrNoRows) {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return fmt.Errorf("resolve duplicate job after unique conflict: %w", queryErr)
			}
			return errJobDedupeRace
		}
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) checkJobQuota(ctx context.Context, tx pgx.Tx, userID string) error {
	if s.maxActiveJobsPerUser > 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE user_id=$1 AND status IN ('queued','running')`, userID).Scan(&count); err != nil {
			return err
		}
		if count >= s.maxActiveJobsPerUser {
			return fmt.Errorf("%w: 每个用户最多 %d 个排队或运行中的任务", ErrJobQuotaExceeded, s.maxActiveJobsPerUser)
		}
	}
	if s.maxActiveJobs > 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE status IN ('queued','running')`).Scan(&count); err != nil {
			return err
		}
		if count >= s.maxActiveJobs {
			return fmt.Errorf("%w: 系统最多 %d 个排队或运行中的任务", ErrJobQuotaExceeded, s.maxActiveJobs)
		}
	}
	return nil
}

func jobDedupeKey(key []byte, fields ...string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("wvc-job-dedupe-v1\x00"))
	for _, field := range fields {
		_, _ = fmt.Fprintf(mac, "%d:", len(field))
		_, _ = mac.Write([]byte(field))
	}
	return fmt.Sprintf("%x", mac.Sum(nil))
}

var errJobResourceContention = errors.New("job resource lock changed during claim")

func (s *Store) ClaimJob(ctx context.Context, workerID string) (*Job, error) {
	// Two workers can snapshot the same queued job with no running peer, then
	// race to become the first runner. The partial unique index on resource_key
	// picks a winner; the loser starts a fresh snapshot and tries again.
	for attempt := 0; attempt < 4; attempt++ {
		job, err := s.claimJobOnce(ctx, workerID)
		if errors.Is(err, errJobResourceContention) {
			continue
		}
		return job, err
	}
	return nil, errors.New("job resource contention did not settle; please retry")
}

func (s *Store) claimJobOnce(ctx context.Context, workerID string) (*Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var j Job
	err = tx.QueryRow(ctx, `SELECT id,user_id,COALESCE(host_id,''),kind,label,tool,args_json::text,stdin,
		read_only,timeout_seconds,status,cancel_requested,created_at,sensitive,dedupe_key,
		args_ciphertext,stdin_ciphertext,resource_key
		FROM jobs j WHERE j.status='queued'
		AND (j.resource_key='' OR NOT EXISTS (
			SELECT 1 FROM jobs r WHERE r.status='running' AND r.resource_key=j.resource_key
		))
		ORDER BY j.created_at, j.id
		FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&j.ID, &j.UserID, &j.HostID, &j.Kind, &j.Label, &j.Tool, &j.ArgsJSON, &j.Stdin,
			&j.ReadOnly, &j.TimeoutSeconds, &j.Status, &j.CancelRequested, &j.CreatedAt,
			&j.Sensitive, &j.DedupeKey, &j.argsCiphertext, &j.stdinCiphertext, &j.ResourceKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.decryptJob(&j); err != nil {
		if s.cipher == nil {
			return nil, errors.New("job encryption key is not configured")
		}
		message := "任务加密数据无法解密，已拒绝执行"
		errorCipher, sealErr := s.cipher.SealFor("job:"+j.ID+":error", message)
		if sealErr != nil {
			return nil, sealErr
		}
		if _, updateErr := tx.Exec(ctx, `UPDATE jobs SET status='failed', error='', error_ciphertext=$2, finished_at=now(), heartbeat_at=now()
			WHERE id=$1 AND status='queued'`, j.ID, errorCipher); updateErr != nil {
			return nil, updateErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return nil, fmt.Errorf("job %s payload cannot be decrypted: %w", j.ID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET status='running', started_at=now(), heartbeat_at=now(), worker_id=$2, attempts=attempts+1
		WHERE id=$1`, j.ID, workerID); err != nil {
		if IsUniqueViolation(err) && j.ResourceKey != "" {
			return nil, errJobResourceContention
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	j.Status = "running"
	return &j, nil
}

func (s *Store) HeartbeatJob(ctx context.Context, id, workerID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at=now() WHERE id=$1 AND status='running' AND worker_id=$2`, id, workerID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) FinishJob(ctx context.Context, id, workerID, status, output, errText string) (bool, error) {
	if status != "succeeded" && status != "failed" && status != "cancelled" {
		return false, fmt.Errorf("invalid final job status %q", status)
	}
	if s.cipher == nil {
		return false, errors.New("job encryption key is not configured")
	}
	outputCipher, errorCipher := "", ""
	if output != "" {
		var err error
		outputCipher, err = s.cipher.SealFor("job:"+id+":output", output)
		if err != nil {
			return false, err
		}
	}
	if errText != "" {
		var err error
		errorCipher, err = s.cipher.SealFor("job:"+id+":error", errText)
		if err != nil {
			return false, err
		}
	}
	output, errText = "", ""
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET status=$3, output=$4, error=$5, output_ciphertext=$6,
		error_ciphertext=$7, finished_at=now(), heartbeat_at=now()
		WHERE id=$1 AND status='running' AND worker_id=$2`, id, workerID, status, output, errText, outputCipher, errorCipher)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	var j Job
	err := s.pool.QueryRow(ctx, `SELECT j.id,j.user_id,u.username,COALESCE(j.host_id,''),COALESCE(h.name,''),
		j.kind,j.label,j.tool,j.args_json::text,j.stdin,j.read_only,j.timeout_seconds,j.status,j.output,j.error,
		j.cancel_requested,j.created_at,COALESCE(j.started_at,'epoch'::timestamptz),COALESCE(j.finished_at,'epoch'::timestamptz),
		j.sensitive,j.dedupe_key,j.resource_key,j.args_ciphertext,j.stdin_ciphertext,j.output_ciphertext,j.error_ciphertext
		FROM jobs j JOIN users u ON u.id=j.user_id LEFT JOIN hosts h ON h.id=j.host_id WHERE j.id=$1`, id).
		Scan(&j.ID, &j.UserID, &j.UserName, &j.HostID, &j.HostName,
			&j.Kind, &j.Label, &j.Tool, &j.ArgsJSON, &j.Stdin, &j.ReadOnly, &j.TimeoutSeconds,
			&j.Status, &j.Output, &j.Error, &j.CancelRequested, &j.CreatedAt, &j.StartedAt, &j.FinishedAt,
			&j.Sensitive, &j.DedupeKey, &j.ResourceKey, &j.argsCiphertext, &j.stdinCiphertext, &j.outputCiphertext, &j.errorCiphertext)
	if err != nil {
		return j, err
	}
	if err := s.decryptJob(&j); err != nil {
		return j, err
	}
	return j, nil
}

func (s *Store) decryptJob(j *Job) error {
	if j.argsCiphertext != "" {
		if s.cipher == nil {
			return errors.New("job encryption key is not configured")
		}
		plain, err := s.cipher.OpenFor("job:"+j.ID+":args", j.argsCiphertext)
		if err != nil {
			return fmt.Errorf("decrypt job arguments: %w", err)
		}
		j.ArgsJSON = plain
	}
	if j.stdinCiphertext != "" {
		if s.cipher == nil {
			return errors.New("job encryption key is not configured")
		}
		plain, err := s.cipher.OpenFor("job:"+j.ID+":stdin", j.stdinCiphertext)
		if err != nil {
			return fmt.Errorf("decrypt job stdin: %w", err)
		}
		j.Stdin = plain
	}
	if j.outputCiphertext != "" {
		if s.cipher == nil {
			return errors.New("job encryption key is not configured")
		}
		plain, err := s.cipher.OpenFor("job:"+j.ID+":output", j.outputCiphertext)
		if err != nil {
			return fmt.Errorf("decrypt job output: %w", err)
		}
		j.Output = plain
	}
	if j.errorCiphertext != "" {
		if s.cipher == nil {
			return errors.New("job encryption key is not configured")
		}
		plain, err := s.cipher.OpenFor("job:"+j.ID+":error", j.errorCiphertext)
		if err != nil {
			return fmt.Errorf("decrypt job error: %w", err)
		}
		j.Error = plain
	}
	return nil
}

func (s *Store) ListJobs(ctx context.Context, userID string, isAdmin bool, limit int) ([]Job, error) {
	return s.ListJobsPage(ctx, userID, isAdmin, limit, 0)
}

func (s *Store) ListJobsPage(ctx context.Context, userID string, isAdmin bool, limit, offset int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT j.id,j.user_id,u.username,COALESCE(j.host_id,''),COALESCE(h.name,''),
		j.kind,j.label,j.tool,j.status,j.error,j.created_at,
		COALESCE(j.started_at,'epoch'::timestamptz),COALESCE(j.finished_at,'epoch'::timestamptz),j.sensitive
		FROM jobs j JOIN users u ON u.id=j.user_id LEFT JOIN hosts h ON h.id=j.host_id`
	args := []any{}
	if !isAdmin {
		query += ` WHERE j.user_id=$1`
		args = append(args, userID)
	}
	query += fmt.Sprintf(` ORDER BY j.created_at DESC LIMIT %d OFFSET %d`, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.UserID, &j.UserName, &j.HostID, &j.HostName,
			&j.Kind, &j.Label, &j.Tool, &j.Status, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.Sensitive); err != nil {
			return nil, err
		}
		protectJobSummary(&j, isAdmin)
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) ListJobsByHost(ctx context.Context, userID string, isAdmin bool, hostID string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	query := `SELECT j.id,j.user_id,u.username,COALESCE(j.host_id,''),COALESCE(h.name,''),
		j.kind,j.label,j.tool,j.status,j.error,j.created_at,
		COALESCE(j.started_at,'epoch'::timestamptz),COALESCE(j.finished_at,'epoch'::timestamptz),j.sensitive
		FROM jobs j JOIN users u ON u.id=j.user_id LEFT JOIN hosts h ON h.id=j.host_id WHERE j.host_id=$1`
	args := []any{hostID}
	if !isAdmin {
		query += ` AND j.user_id=$2`
		args = append(args, userID)
	}
	query += fmt.Sprintf(` ORDER BY j.created_at DESC LIMIT %d`, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.UserID, &j.UserName, &j.HostID, &j.HostName,
			&j.Kind, &j.Label, &j.Tool, &j.Status, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.Sensitive); err != nil {
			return nil, err
		}
		protectJobSummary(&j, isAdmin)
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// protectJobSummary is deliberately conservative for command jobs. Rows
// created before the sensitive column was introduced may not have the flag
// even though their label contains command arguments, so list endpoints must
// not expose any legacy command label to non-admins.
func protectJobSummary(job *Job, isAdmin bool) {
	if isAdmin || (!job.Sensitive && job.Kind != "command") {
		return
	}
	job.Label = job.Tool + " [敏感参数已隐藏]"
	job.Error = ""
}

func (s *Store) RequestCancelJob(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET status='cancelled', finished_at=now(), cancel_requested=true
		WHERE id=$1 AND status='queued'`, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	tag, err = s.pool.Exec(ctx, `UPDATE jobs SET cancel_requested=true WHERE id=$1 AND status='running'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) JobCancelRequested(ctx context.Context, id string) (bool, error) {
	var cancel bool
	err := s.pool.QueryRow(ctx, `SELECT cancel_requested FROM jobs WHERE id=$1`, id).Scan(&cancel)
	return cancel, err
}

func (s *Store) RecoverStaleJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	if s.cipher == nil {
		return 0, errors.New("job encryption key is not configured")
	}
	const message = "worker lost; job was recovered after restart"
	var total int64
	for {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return total, err
		}
		rows, err := tx.Query(ctx, `SELECT j.id,j.user_id,u.username,COALESCE(j.host_id,''),j.kind,j.tool,j.label,j.sensitive
			FROM jobs j JOIN users u ON u.id=j.user_id
			WHERE j.status='running' AND COALESCE(j.heartbeat_at,j.started_at) < now() - $1::interval
			ORDER BY j.created_at FOR UPDATE OF j SKIP LOCKED LIMIT 500`,
			fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
		if err != nil {
			_ = tx.Rollback(ctx)
			return total, err
		}
		type staleJob struct {
			id, userID, userName, hostID, kind, tool, label string
			sensitive                                       bool
		}
		var stale []staleJob
		for rows.Next() {
			var job staleJob
			if err := rows.Scan(&job.id, &job.userID, &job.userName, &job.hostID,
				&job.kind, &job.tool, &job.label, &job.sensitive); err != nil {
				rows.Close()
				_ = tx.Rollback(ctx)
				return total, err
			}
			stale = append(stale, job)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			_ = tx.Rollback(ctx)
			return total, err
		}
		if len(stale) == 0 {
			if err := tx.Commit(ctx); err != nil {
				return total, err
			}
			return total, nil
		}
		for _, job := range stale {
			errorCipher, err := s.cipher.SealFor("job:"+job.id+":error", message)
			if err != nil {
				_ = tx.Rollback(ctx)
				return total, err
			}
			tag, err := tx.Exec(ctx, `UPDATE jobs SET status='failed', error='', error_ciphertext=$2,
				finished_at=now(), heartbeat_at=now() WHERE id=$1 AND status='running'`, job.id, errorCipher)
			if err != nil {
				_ = tx.Rollback(ctx)
				return total, err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			label := job.label
			if job.sensitive || job.kind == "command" {
				label = job.tool + " [敏感参数已隐藏]"
			}
			detail, _ := json.Marshal(map[string]any{
				"job_id": job.id, "label": label, "tool": job.tool,
				"status": "failed", "reason": message,
			})
			if _, err := tx.Exec(ctx, `INSERT INTO audit_outbox(
				source_id,actor_id,actor_name,action,host_id,detail,created_at)
				VALUES($1,$2,$3,'job.recovered',$4,$5::jsonb,now())
				ON CONFLICT(source_id) DO NOTHING`,
				"job-recovered:"+job.id, job.userID, job.userName, job.hostID, string(detail)); err != nil {
				_ = tx.Rollback(ctx)
				return total, err
			}
			total++
		}
		if err := tx.Commit(ctx); err != nil {
			return total, err
		}
		if len(stale) < 500 {
			return total, nil
		}
	}
}

func (s *Store) QueueStats(ctx context.Context) (QueueStats, error) {
	var out QueueStats
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM jobs GROUP BY status`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return out, err
		}
		switch status {
		case "queued":
			out.Queued = count
		case "running":
			out.Running = count
		case "succeeded":
			out.Succeeded = count
		case "failed":
			out.Failed = count
		case "cancelled":
			out.Cancelled = count
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, err
}

func (s *Store) PurgeOldJobs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM jobs
		WHERE finished_at IS NOT NULL AND finished_at < $1
		AND status IN ('succeeded','failed','cancelled')`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) PurgeOldAudit(ctx context.Context, before time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditLock); err != nil {
		return 0, err
	}
	// The hash chain can only be truncated from its oldest end. A replayed
	// outbox event may have an old created_at but a new id, so selecting the
	// maximum id below the time cutoff could delete newer evidence that sits
	// between two old records. Find the first retained row by id and remove
	// only the contiguous prefix before it.
	var retainedID int64
	err = tx.QueryRow(ctx, `SELECT id FROM audit_logs WHERE created_at >= $1 ORDER BY id LIMIT 1`, before).Scan(&retainedID)
	var cutoff int64
	switch {
	case err == nil:
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(id),0) FROM audit_logs WHERE id < $1`, retainedID).Scan(&cutoff); err != nil {
			return 0, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(id),0) FROM audit_logs`).Scan(&cutoff); err != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	if cutoff == 0 {
		return 0, tx.Commit(ctx)
	}
	// Preserve the exact predecessor of the first retained row. Empty hashes
	// are retained as empty so legacy rows remain distinguishable from a
	// missing anchor.
	var anchor string
	err = tx.QueryRow(ctx, `SELECT entry_hash FROM audit_logs WHERE id=$1`, cutoff).Scan(&anchor)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM audit_logs WHERE id <= $1`, cutoff)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM audit_chain_checkpoints`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_chain_checkpoints(cutoff_id,entry_hash) VALUES($1,$2)`, cutoff, anchor); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) UpsertWorkerHeartbeat(ctx context.Context, workerID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO worker_heartbeats(worker_id,last_seen)
		VALUES($1,now()) ON CONFLICT(worker_id) DO UPDATE SET last_seen=now()`, workerID)
	return err
}

func (s *Store) CountLiveWorkers(ctx context.Context, maxAge time.Duration) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM worker_heartbeats WHERE last_seen > now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(maxAge.Seconds()))).Scan(&count)
	return count, err
}

func (s *Store) PurgeWorkerHeartbeats(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM worker_heartbeats WHERE last_seen < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) AddAudit(ctx context.Context, a Audit) error {
	if s.cipher == nil {
		return errors.New("audit chain key is not configured")
	}
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		detail = []byte(`{}`)
	}
	createdAt := a.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditLock); err != nil {
		return err
	}
	var prevHash string
	if err := tx.QueryRow(ctx, `SELECT entry_hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prevHash); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	} else if errors.Is(err, pgx.ErrNoRows) {
		if checkpointErr := tx.QueryRow(ctx, `SELECT entry_hash FROM audit_chain_checkpoints ORDER BY cutoff_id DESC LIMIT 1`).Scan(&prevHash); checkpointErr != nil && !errors.Is(checkpointErr, pgx.ErrNoRows) {
			return checkpointErr
		}
	}
	var id int64
	var storedDetail string
	var storedCreated time.Time
	err = tx.QueryRow(ctx, `INSERT INTO audit_logs(actor_id,actor_name,action,host_id,vm_name,detail,remote_ip,created_at,source_id,prev_hash,entry_hash)
		VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,'')
		RETURNING id, detail::text, created_at`,
		a.ActorID, a.ActorName, a.Action, a.HostID, a.VMName, string(detail), a.RemoteIP, createdAt, a.SourceID, prevHash).
		Scan(&id, &storedDetail, &storedCreated)
	if err != nil {
		if a.SourceID != "" && IsUniqueViolation(err) {
			return ErrAuditDuplicate
		}
		return err
	}
	entryHash, err := s.auditHash(prevHash, a, storedDetail, storedCreated.UTC())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE audit_logs SET entry_hash=$2 WHERE id=$1`, id, entryHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddAuditReliable tries the hash-chain write first and falls back to the
// durable outbox on transient storage or lock failures. The outbox drainer
// retries later with the same source_id, so a crash between the two writes
// cannot produce two audit records.
func (s *Store) AddAuditReliable(ctx context.Context, a Audit) error {
	if a.SourceID == "" {
		a.SourceID = id.New("audit")
	}
	err := s.AddAudit(ctx, a)
	if err == nil || errors.Is(err, ErrAuditDuplicate) {
		return nil
	}
	if queueErr := s.EnqueueAudit(ctx, a); queueErr != nil {
		return fmt.Errorf("audit chain write failed (%v); outbox write also failed: %w", err, queueErr)
	}
	return nil
}

func (s *Store) EnqueueAudit(ctx context.Context, a Audit) error {
	if a.SourceID == "" {
		a.SourceID = id.New("audit")
	}
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		detail = []byte(`{}`)
	}
	createdAt := a.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO audit_outbox(
		source_id,actor_id,actor_name,action,host_id,vm_name,detail,remote_ip,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)
		ON CONFLICT(source_id) DO NOTHING`,
		a.SourceID, a.ActorID, a.ActorName, a.Action, a.HostID, a.VMName, string(detail), a.RemoteIP, createdAt)
	return err
}

type auditOutboxRow struct {
	ID       int64
	Audit    Audit
	Attempts int
}

// ProcessAuditOutbox drains a bounded batch of durable audit events. It is
// safe to run in every application replica. Rows are claimed atomically with
// FOR UPDATE SKIP LOCKED and a short lease, so replicas do not duplicate work;
// source_id remains the final idempotency boundary if a process crashes after
// writing the audit chain but before deleting the outbox row.
func (s *Store) ProcessAuditOutbox(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `WITH claimed AS (
		UPDATE audit_outbox
		SET next_attempt_at=now()+interval '60 seconds'
		WHERE id IN (
			SELECT id FROM audit_outbox
			WHERE next_attempt_at <= now()
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id,source_id,actor_id,actor_name,action,host_id,vm_name,
			detail::text,remote_ip,created_at,attempts
	)
	SELECT id,source_id,actor_id,actor_name,action,host_id,vm_name,
		detail,remote_ip,created_at,attempts
	FROM claimed ORDER BY id`, limit)
	if err != nil {
		return 0, err
	}
	batch := make([]auditOutboxRow, 0, limit)
	for rows.Next() {
		var row auditOutboxRow
		var detail string
		if err := rows.Scan(&row.ID, &row.Audit.SourceID, &row.Audit.ActorID, &row.Audit.ActorName,
			&row.Audit.Action, &row.Audit.HostID, &row.Audit.VMName, &detail, &row.Audit.RemoteIP,
			&row.Audit.CreatedAt, &row.Attempts); err != nil {
			rows.Close()
			return 0, err
		}
		if err := json.Unmarshal([]byte(detail), &row.Audit.Detail); err != nil {
			row.Audit.Detail = map[string]any{}
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	processed := 0
	failed := 0
	var firstErr error
	for _, row := range batch {
		err := s.AddAudit(ctx, row.Audit)
		if err == nil || errors.Is(err, ErrAuditDuplicate) {
			if _, deleteErr := s.pool.Exec(ctx, `DELETE FROM audit_outbox WHERE id=$1`, row.ID); deleteErr != nil {
				return processed, deleteErr
			}
			processed++
			continue
		}
		backoff := time.Second << min(row.Attempts, 8)
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		message := err.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		if _, updateErr := s.pool.Exec(ctx, `UPDATE audit_outbox SET attempts=attempts+1,
			last_error=$2, next_attempt_at=now()+$3::interval WHERE id=$1`,
			row.ID, message, fmt.Sprintf("%d seconds", int(backoff.Seconds()))); updateErr != nil {
			return processed, updateErr
		}
		failed++
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return processed, fmt.Errorf("failed to process %d audit outbox event(s): %w", failed, firstErr)
	}
	return processed, nil
}

func (s *Store) AuditOutboxStats(ctx context.Context) (AuditOutboxStats, error) {
	var stats AuditOutboxStats
	err := s.pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE next_attempt_at <= now()),
		count(*) FILTER (WHERE attempts > 0)
		FROM audit_outbox`).Scan(&stats.Total, &stats.Due, &stats.Failed)
	return stats, err
}

func (s *Store) RecentAudit(ctx context.Context, limit int) ([]Audit, error) {
	return s.RecentAuditPage(ctx, limit, 0)
}

func (s *Store) RecentAuditPage(ctx context.Context, limit, offset int) ([]Audit, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT actor_id,actor_name,action,host_id,vm_name,detail::text,remote_ip,created_at
		FROM audit_logs ORDER BY id DESC LIMIT %d OFFSET %d`, limit, offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		var a Audit
		var detail string
		if err := rows.Scan(&a.ActorID, &a.ActorName, &a.Action, &a.HostID, &a.VMName, &detail, &a.RemoteIP, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Detail = map[string]any{}
		_ = json.Unmarshal([]byte(detail), &a.Detail)
		out = append(out, a)
	}
	return out, rows.Err()
}

func auditHashLegacy(prev string, a Audit, detail string, createdAt time.Time) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d",
		prev, a.ActorID, a.ActorName, a.Action, a.HostID, a.VMName, detail, a.RemoteIP, createdAt.UnixNano())
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *Store) auditHash(prev string, a Audit, detail string, createdAt time.Time) (string, error) {
	if s.cipher == nil {
		return "", errors.New("audit chain key is not configured")
	}
	h := hmac.New(sha256.New, s.cipher.DerivedKey("audit-chain"))
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d",
		prev, a.ActorID, a.ActorName, a.Action, a.HostID, a.VMName, detail, a.RemoteIP, createdAt.UnixNano())
	return "hmac-sha256:" + fmt.Sprintf("%x", h.Sum(nil)), nil
}

// VerifyAuditChain checks every hash-chained row. Rows written before this
// feature was introduced (empty entry_hash) are accepted only before the
// chain starts. Checkpoints created by retention cleanup supply the anchor
// for the first retained row.
func (s *Store) VerifyAuditChain(ctx context.Context) error {
	if s.cipher == nil {
		return errors.New("audit chain key is not configured")
	}
	// A single repeatable-read snapshot keeps the checkpoint and all audit rows
	// mutually consistent even if another replica appends or purges audit
	// records while this process is starting. It also avoids blocking writers
	// for the duration of a full history scan.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var checkpoint string
	if err := tx.QueryRow(ctx, `SELECT entry_hash FROM audit_chain_checkpoints ORDER BY cutoff_id DESC LIMIT 1`).Scan(&checkpoint); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT actor_id,actor_name,action,host_id,vm_name,detail::text,remote_ip,created_at,prev_hash,entry_hash
		FROM audit_logs ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	prev := checkpoint
	seenChain := false
	hmacStarted := strings.HasPrefix(checkpoint, "hmac-sha256:")
	for rows.Next() {
		var a Audit
		var detail, storedPrev, storedHash string
		if err := rows.Scan(&a.ActorID, &a.ActorName, &a.Action, &a.HostID, &a.VMName, &detail, &a.RemoteIP, &a.CreatedAt, &storedPrev, &storedHash); err != nil {
			return err
		}
		if storedHash == "" {
			if seenChain || checkpoint != "" {
				return errors.New("audit hash chain contains an unchained row after the chain started")
			}
			continue
		}
		expected := ""
		if strings.HasPrefix(storedHash, "hmac-sha256:") {
			hmacStarted = true
			var hashErr error
			expected, hashErr = s.auditHash(storedPrev, a, detail, a.CreatedAt.UTC())
			if hashErr != nil {
				return hashErr
			}
		} else {
			// A DB writer without the master key must not be able to append
			// an unkeyed legacy hash after the authenticated chain has begun.
			if hmacStarted {
				return errors.New("audit hash chain downgraded from HMAC to legacy SHA-256")
			}
			expected = auditHashLegacy(storedPrev, a, detail, a.CreatedAt.UTC())
		}
		if storedPrev != prev || !hmac.Equal([]byte(expected), []byte(storedHash)) {
			return fmt.Errorf("audit hash chain verification failed at %s", a.CreatedAt.Format(time.RFC3339Nano))
		}
		prev = storedHash
		seenChain = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	return tx.Commit(ctx)
}

func IsUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func validUserRole(role string) bool {
	return role == "admin" || role == "operator" || role == "desktop" || role == "viewer"
}

func validJobKind(kind string) bool {
	switch kind {
	case "command", "vm-create", "vm-clone", "vm-action", "host-test", "filesystem-inspect",
		"storage-action", "network-action", "disk-action", "vm-resource":
		return true
	default:
		return false
	}
}
