ALTER TABLE backup_policies ADD COLUMN IF NOT EXISTS target_kind TEXT NOT NULL DEFAULT 'local';
ALTER TABLE backup_policies ADD COLUMN IF NOT EXISTS target_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_policies DROP CONSTRAINT IF EXISTS backup_policies_target_kind_check;
ALTER TABLE backup_policies ADD CONSTRAINT backup_policies_target_kind_check
    CHECK (target_kind IN ('local', 'nfs', 's3'));

CREATE TABLE IF NOT EXISTS backup_drills (
    id          TEXT PRIMARY KEY,
    host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name     TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_ref  TEXT NOT NULL DEFAULT '',
    checksum    TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL CHECK (result IN ('passed', 'failed')),
    note        TEXT NOT NULL DEFAULT '',
    actor_name  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (char_length(note) <= 500)
);

CREATE INDEX IF NOT EXISTS backup_drills_vm_idx ON backup_drills (host_id, vm_name, created_at DESC);
