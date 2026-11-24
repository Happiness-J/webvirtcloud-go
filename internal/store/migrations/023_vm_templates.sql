CREATE TABLE IF NOT EXISTS vm_templates (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    vcpus      INTEGER NOT NULL,
    memory_gb  INTEGER NOT NULL,
    disk_gb    INTEGER NOT NULL,
    network    TEXT NOT NULL DEFAULT '',
    os_variant TEXT NOT NULL DEFAULT 'generic',
    hostname   TEXT NOT NULL DEFAULT '',
    username   TEXT NOT NULL DEFAULT '',
    address    TEXT NOT NULL DEFAULT '',
    gateway    TEXT NOT NULL DEFAULT '',
    dns        TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (char_length(name) BETWEEN 1 AND 64)
);
