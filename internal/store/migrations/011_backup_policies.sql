CREATE TABLE IF NOT EXISTS backup_policies (
    id            TEXT PRIMARY KEY,
    host_id       TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name       TEXT NOT NULL,
    interval_hours INTEGER NOT NULL CHECK (interval_hours BETWEEN 1 AND 168),
    retain_count  INTEGER NOT NULL CHECK (retain_count BETWEEN 1 AND 100),
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at   TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (host_id, vm_name)
);
