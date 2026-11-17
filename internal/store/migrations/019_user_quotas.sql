CREATE TABLE IF NOT EXISTS user_quotas (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    max_vms     INTEGER NOT NULL DEFAULT 20 CHECK (max_vms BETWEEN 0 AND 10000),
    max_vcpus   INTEGER NOT NULL DEFAULT 64 CHECK (max_vcpus BETWEEN 0 AND 100000),
    max_memory_gb INTEGER NOT NULL DEFAULT 256 CHECK (max_memory_gb BETWEEN 0 AND 1000000),
    max_disk_gb INTEGER NOT NULL DEFAULT 2000 CHECK (max_disk_gb BETWEEN 0 AND 10000000)
);

CREATE TABLE IF NOT EXISTS vm_usage (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    host_id    TEXT NOT NULL,
    vm_name    TEXT NOT NULL,
    vcpus      INTEGER NOT NULL,
    memory_gb  INTEGER NOT NULL,
    disk_gb    INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (host_id, vm_name)
);
