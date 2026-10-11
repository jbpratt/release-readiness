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

-- name: ListKonfluxReleasesByApplication :many
SELECT id, name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, start_time, completion_time
FROM konflux_releases
WHERE application = ?
ORDER BY created_at DESC, id DESC;

-- name: ListReleasedSnapshotImages :many
-- The component images of each stored Snapshot a Release of the application names.
SELECT DISTINCT s.name, sc.image_url
FROM konflux_releases kr
JOIN snapshots s ON s.name = kr.snapshot AND s.application = kr.application
JOIN snapshot_components sc ON sc.snapshot_id = s.id
WHERE kr.application = ?;
