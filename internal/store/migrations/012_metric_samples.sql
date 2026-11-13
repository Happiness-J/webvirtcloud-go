CREATE TABLE IF NOT EXISTS metric_samples (
    id BIGSERIAL PRIMARY KEY,
    host_id TEXT NOT NULL,
    vm_name TEXT NOT NULL,
    cpu_percent DOUBLE PRECISION NOT NULL,
    memory_percent DOUBLE PRECISION NOT NULL,
    disk_read_bps BIGINT NOT NULL,
    disk_write_bps BIGINT NOT NULL,
    sampled_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS metric_samples_lookup_idx ON metric_samples (host_id, vm_name, sampled_at DESC);
