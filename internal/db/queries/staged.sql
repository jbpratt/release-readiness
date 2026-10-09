-- name: UpsertStagedSnapshot :exec
INSERT INTO staged_snapshots (name, assembly, kind, env, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    assembly=excluded.assembly,
    kind=excluded.kind,
    env=excluded.env,
    created_at=excluded.created_at;

-- name: LatestStagedSnapshot :one
SELECT name, assembly, kind, env, created_at
FROM staged_snapshots
WHERE assembly = ? AND kind = ? AND env = 'stage'
ORDER BY created_at DESC, name DESC
LIMIT 1;

-- name: ListSuccessfulStageReleases :many
SELECT kr.name AS release_name, kr.release_plan, kr.completion_time,
       s.id AS snapshot_id, s.name AS snapshot_name, s.created_at AS snapshot_created_at
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN konflux_releases kr ON kr.snapshot = s.name AND kr.application = s.application
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'image' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
  AND kr.released_status = 'True' AND kr.released_reason = 'Succeeded' AND kr.completion_time != ''
ORDER BY kr.completion_time DESC, kr.created_at DESC, kr.name DESC;
