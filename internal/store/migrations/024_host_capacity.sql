CREATE TABLE IF NOT EXISTS host_capacity_samples (
    host_id      TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    cpu_percent  DOUBLE PRECISION NOT NULL,
    mem_percent  DOUBLE PRECISION NOT NULL,
    running_vms  INTEGER NOT NULL,
    sampled_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS host_capacity_samples_lookup_idx
    ON host_capacity_samples (host_id, sampled_at DESC);
