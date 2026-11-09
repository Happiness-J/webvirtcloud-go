-- Security events are written to a durable outbox when the primary audit
-- chain is temporarily unavailable. source_id makes retries idempotent even
-- when multiple application replicas drain the queue concurrently.
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS audit_logs_source_id_unique_idx
    ON audit_logs (source_id) WHERE source_id <> '';

CREATE TABLE IF NOT EXISTS audit_outbox (
    id              BIGSERIAL PRIMARY KEY,
    source_id       TEXT NOT NULL UNIQUE,
    actor_id        TEXT NOT NULL DEFAULT '',
    actor_name      TEXT NOT NULL DEFAULT '',
    action          TEXT NOT NULL,
    host_id         TEXT NOT NULL DEFAULT '',
    vm_name         TEXT NOT NULL DEFAULT '',
    detail          JSONB NOT NULL DEFAULT '{}'::jsonb,
    remote_ip       TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_outbox_next_attempt_idx
    ON audit_outbox (next_attempt_at, id);
