-- name: UpsertKonfluxRelease :exec
INSERT INTO konflux_releases (name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, start_time, completion_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    application=excluded.application,
    snapshot=excluded.snapshot,
    release_plan=excluded.release_plan,
    released_status=excluded.released_status,
    released_reason=excluded.released_reason,
    failed_task=excluded.failed_task,
    failed_step=excluded.failed_step,
    created_at=excluded.created_at,
    start_time=excluded.start_time,
    completion_time=excluded.completion_time;

-- name: ListKonfluxReleasesBySnapshots :many
SELECT id, name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, start_time, completion_time
FROM konflux_releases
WHERE snapshot IN (sqlc.slice('snapshots'))
ORDER BY created_at DESC, id DESC;
