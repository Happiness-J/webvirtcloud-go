ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS user_host_access (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    host_id    TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, host_id)
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    role         TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'desktop', 'viewer')),
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (char_length(name) BETWEEN 1 AND 64)
);

CREATE INDEX IF NOT EXISTS api_tokens_user_idx ON api_tokens (user_id, created_at DESC);
