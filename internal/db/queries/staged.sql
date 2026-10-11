-- name: UpsertStagedSnapshot :exec
INSERT INTO staged_snapshots (name, assembly, kind, env, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    assembly=excluded.assembly,
    kind=excluded.kind,
    env=excluded.env,
    created_at=excluded.created_at;

-- name: ListSuccessfulStageReleases :many
SELECT kr.release_plan, kr.completion_time, s.name AS snapshot_name
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN konflux_releases kr ON kr.snapshot = s.name AND kr.application = s.application
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'image' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
  AND kr.released_status = 'True' AND kr.released_reason = 'Succeeded' AND kr.completion_time != ''
ORDER BY kr.completion_time DESC, kr.created_at DESC, kr.name DESC;

-- name: LatestProdImageRelease :one
SELECT id, name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, start_time, completion_time
FROM konflux_releases
WHERE snapshot IN (SELECT name FROM staged_snapshots WHERE assembly = ? AND kind = 'image' AND env = 'prod')
ORDER BY created_at DESC, id DESC
LIMIT 1;
