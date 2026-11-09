-- Retain a cryptographic anchor when old, hash-chained audit rows are purged
-- according to the configured retention period. Without this checkpoint the
-- next retained row would reference a deleted predecessor and verification
-- would fail after routine maintenance.
CREATE TABLE IF NOT EXISTS audit_chain_checkpoints (
    cutoff_id  BIGINT PRIMARY KEY,
    entry_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
