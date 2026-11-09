-- Make job idempotency safe under concurrent enqueue attempts.
UPDATE jobs
SET dedupe_key = ''
WHERE id IN (
    SELECT id FROM (
        SELECT id, row_number() OVER (PARTITION BY dedupe_key ORDER BY created_at, id) AS rn
        FROM jobs
        WHERE dedupe_key <> '' AND status IN ('queued', 'running')
    ) duplicates
    WHERE rn > 1
);
DROP INDEX IF EXISTS jobs_active_dedupe_idx;
CREATE UNIQUE INDEX IF NOT EXISTS jobs_active_dedupe_unique_idx
    ON jobs (dedupe_key)
    WHERE status IN ('queued', 'running') AND dedupe_key <> '';

-- Workers publish a heartbeat so readiness checks do not report green when
-- the web process is alive but no job consumer is running.
CREATE TABLE IF NOT EXISTS worker_heartbeats (
    worker_id TEXT PRIMARY KEY,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS worker_heartbeats_last_seen_idx ON worker_heartbeats (last_seen DESC);

-- A hash chain makes accidental or application-level audit tampering evident.
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS entry_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS audit_entry_hash_idx ON audit_logs (entry_hash);
