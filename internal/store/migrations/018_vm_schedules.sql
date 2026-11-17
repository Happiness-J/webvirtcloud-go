CREATE TABLE IF NOT EXISTS vm_schedules (
    id             TEXT PRIMARY KEY,
    host_id        TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name        TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('snapshot', 'power')),
    interval_hours INTEGER NOT NULL CHECK (interval_hours BETWEEN 1 AND 168),
    retain_count   INTEGER NOT NULL DEFAULT 7 CHECK (retain_count BETWEEN 1 AND 100),
    power_action   TEXT NOT NULL DEFAULT '' CHECK (power_action IN ('', 'start', 'shutdown')),
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at    TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (host_id, vm_name, kind)
);
