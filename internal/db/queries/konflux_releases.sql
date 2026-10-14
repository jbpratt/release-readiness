-- name: UpsertKonfluxRelease :exec
INSERT INTO konflux_releases (name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, completion_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    application=excluded.application,
    snapshot=excluded.snapshot,
    release_plan=excluded.release_plan,
    released_status=excluded.released_status,
    released_reason=excluded.released_reason,
    failed_task=excluded.failed_task,
    failed_step=excluded.failed_step,
    created_at=excluded.created_at,
    completion_time=excluded.completion_time;

-- name: UpsertKonfluxReleaseArt :exec
INSERT INTO konflux_release_art (name, job_url, advisory)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    job_url=excluded.job_url,
    advisory=excluded.advisory;

-- name: ListKonfluxReleasesByApplication :many
SELECT sqlc.embed(kr), CAST(COALESCE(a.job_url, '') AS TEXT) AS job_url, CAST(COALESCE(a.advisory, '') AS TEXT) AS advisory
FROM konflux_releases kr
LEFT JOIN konflux_release_art a ON a.name = kr.name
WHERE kr.application = ?
ORDER BY kr.created_at DESC, kr.id DESC;

-- name: ListReleasedSnapshotImages :many
-- The component images of each stored Snapshot a Release of the application names.
SELECT DISTINCT s.name, sc.image_url
FROM konflux_releases kr
JOIN snapshots s ON s.name = kr.snapshot AND s.application = kr.application
JOIN snapshot_components sc ON sc.snapshot_id = s.id
WHERE kr.application = ?;

-- name: ListKonfluxReleases :many
SELECT sqlc.embed(kr), CAST(COALESCE(a.job_url, '') AS TEXT) AS job_url, CAST(COALESCE(a.advisory, '') AS TEXT) AS advisory
FROM konflux_releases kr
LEFT JOIN konflux_release_art a ON a.name = kr.name;
