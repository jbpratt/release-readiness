-- name: UpsertStagedSnapshot :exec
INSERT INTO staged_snapshots (name, assembly, kind, env, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    assembly=excluded.assembly,
    kind=excluded.kind,
    env=excluded.env,
    created_at=excluded.created_at;

-- name: LatestStagedSnapshot :one
-- An empty component matches any Snapshot.
SELECT name, assembly, kind, env, created_at
FROM staged_snapshots ss
WHERE assembly = sqlc.arg(assembly) AND kind = sqlc.arg(kind) AND env = 'stage'
  AND (CAST(sqlc.arg(component) AS TEXT) = '' OR EXISTS (
      SELECT 1
      FROM snapshots s
      JOIN snapshot_components sc ON sc.snapshot_id = s.id
      WHERE s.name = ss.name AND sc.component = sqlc.arg(component)))
ORDER BY created_at DESC, name DESC
LIMIT 1;

-- name: ListSuccessfulStageReleases :many
SELECT kr.release_plan, kr.completion_time, s.id AS snapshot_id, s.name AS snapshot_name
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN konflux_releases kr ON kr.snapshot = s.name AND kr.application = s.application
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'image' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
  AND kr.released_status = 'True' AND kr.released_reason = 'Succeeded' AND kr.completion_time != ''
ORDER BY kr.completion_time DESC, kr.created_at DESC, kr.name DESC;

-- name: StreamStaged :one
-- The '.' after stream X.Y keeps 3.1 from matching assembly 3.18.1.
SELECT CAST(EXISTS (
    SELECT 1 FROM staged_snapshots
    WHERE env = 'stage' AND assembly LIKE CAST(sqlc.arg(stream) AS TEXT) || '.%'
) AS BOOLEAN) AS staged;
