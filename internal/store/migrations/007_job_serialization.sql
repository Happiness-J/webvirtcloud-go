-- Non-read-only jobs that touch the same host or the local QEMU toolchain run
-- one at a time. This prevents conflicting snapshot, destroy, resize, convert
-- and guestfs operations from racing each other.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS resource_key TEXT NOT NULL DEFAULT '';

-- A partial unique index makes the serialization guarantee hold across all
-- application replicas, not just inside one process.
CREATE UNIQUE INDEX IF NOT EXISTS jobs_running_resource_unique_idx
    ON jobs (resource_key)
    WHERE status = 'running' AND resource_key <> '';

-- ClaimJob filters queued work by this index. Read-only jobs keep an empty key
-- and remain eligible for parallel execution.
CREATE INDEX IF NOT EXISTS jobs_queued_resource_idx
    ON jobs (status, resource_key, created_at)
    WHERE status = 'queued';
