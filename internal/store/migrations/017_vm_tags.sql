CREATE TABLE IF NOT EXISTS vm_tags (
    host_id    TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    vm_name    TEXT NOT NULL,
    tag        TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (host_id, vm_name, tag),
    CHECK (char_length(tag) BETWEEN 1 AND 32)
);

CREATE INDEX IF NOT EXISTS vm_tags_tag_idx ON vm_tags (tag);
