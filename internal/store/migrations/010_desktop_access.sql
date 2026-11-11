ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('admin', 'operator', 'desktop', 'viewer'));

CREATE TABLE IF NOT EXISTS user_vm_access (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    host_id    TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, host_id, vm_name),
    CHECK (char_length(vm_name) BETWEEN 1 AND 256)
);

CREATE INDEX IF NOT EXISTS user_vm_access_host_idx
    ON user_vm_access (host_id, vm_name);
