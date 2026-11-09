-- Console tokens are short lived bearer values embedded in WebSocket and
-- password-fetch URLs. Record successful uses so a leaked token cannot be
-- replayed, including when the request lands on another application replica.
CREATE TABLE IF NOT EXISTS console_token_uses (
    token_hash TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS console_token_uses_expires_idx ON console_token_uses (expires_at);
