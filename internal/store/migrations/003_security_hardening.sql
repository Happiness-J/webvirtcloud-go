ALTER TABLE jobs ADD COLUMN IF NOT EXISTS sensitive BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS args_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS stdin_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS output_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS error_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS jobs_status_created_idx ON jobs (status, created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_active_dedupe_idx ON jobs (dedupe_key)
    WHERE status IN ('queued', 'running') AND dedupe_key <> '';

CREATE TABLE IF NOT EXISTS login_attempts (
    key       TEXT PRIMARY KEY,
    failures  INTEGER NOT NULL DEFAULT 0,
    first_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS login_attempts_last_idx ON login_attempts (last_at);
CREATE INDEX IF NOT EXISTS audit_actor_idx ON audit_logs (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_action_idx ON audit_logs (action, created_at DESC);
CREATE INDEX IF NOT EXISTS sessions_last_seen_idx ON sessions (last_seen);
