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
