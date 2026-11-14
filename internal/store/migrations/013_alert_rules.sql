CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL,
    vm_name TEXT NOT NULL DEFAULT '',
    metric TEXT NOT NULL CHECK (metric IN ('cpu', 'memory')),
    threshold DOUBLE PRECISION NOT NULL CHECK (threshold > 0 AND threshold <= 100),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (host_id, vm_name, metric)
);
