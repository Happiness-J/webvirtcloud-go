CREATE TABLE IF NOT EXISTS cluster_policy (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    policy      TEXT NOT NULL DEFAULT 'balanced' CHECK (policy IN ('spread', 'pack', 'balanced')),
    fail_after  INTEGER NOT NULL DEFAULT 3 CHECK (fail_after BETWEEN 2 AND 10),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO cluster_policy(id) VALUES (1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS host_health (
    host_id      TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    reachable    BOOLEAN NOT NULL DEFAULT FALSE,
    failures     INTEGER NOT NULL DEFAULT 0 CHECK (failures >= 0),
    cpu_percent  DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_percent  DOUBLE PRECISION NOT NULL DEFAULT 0,
    running_vms  INTEGER NOT NULL DEFAULT 0,
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ha_vms (
    host_id    TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name    TEXT NOT NULL,
    protected  BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (host_id, vm_name)
);
