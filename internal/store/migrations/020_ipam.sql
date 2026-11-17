CREATE TABLE IF NOT EXISTS ip_pools (
    id         TEXT PRIMARY KEY,
    host_id    TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    cidr       TEXT NOT NULL,
    floating   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (host_id, name)
);

CREATE TABLE IF NOT EXISTS ip_allocations (
    id         TEXT PRIMARY KEY,
    pool_id    TEXT NOT NULL REFERENCES ip_pools(id) ON DELETE CASCADE,
    address    TEXT NOT NULL,
    owner      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pool_id, address)
);
