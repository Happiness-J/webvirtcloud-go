-- Migration 007 added the resource_key column and its unique index, but jobs
-- that were already queued or running kept the empty default. Backfill those
-- rows so an upgrade cannot leave old work outside the serialization domain.
--
-- A legacy database could contain multiple running jobs for the same host
-- because the invariant did not exist before migration 007. The partial unique
-- index cannot represent that state, so keep the oldest running job and fail
-- the later duplicates with an explicit recovery message. Newly queued work is
-- then serialized against the surviving job as usual.
WITH active_running AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY CASE
                   WHEN COALESCE(host_id, '') <> '' THEN 'host:' || host_id
                   ELSE 'local:qemu'
               END
               ORDER BY created_at, id
           ) AS position
    FROM jobs
    WHERE status = 'running' AND NOT read_only
)
UPDATE jobs AS j
SET status = 'failed',
    error = '升级迁移检测到同一资源的多个历史运行中任务，已终止重复任务；请确认宿主机状态后重试',
    finished_at = now(),
    heartbeat_at = now()
FROM active_running AS active
WHERE j.id = active.id AND active.position > 1;

-- Read-only jobs must never hold a serialization key, while every mutating
-- active job must hold exactly the same key the application would assign.
UPDATE jobs
SET resource_key = ''
WHERE status IN ('queued', 'running') AND read_only AND resource_key <> '';

UPDATE jobs
SET resource_key = CASE
    WHEN COALESCE(host_id, '') <> '' THEN 'host:' || host_id
    ELSE 'local:qemu'
END
WHERE status IN ('queued', 'running') AND NOT read_only
  AND resource_key IS DISTINCT FROM CASE
      WHEN COALESCE(host_id, '') <> '' THEN 'host:' || host_id
      ELSE 'local:qemu'
  END;
