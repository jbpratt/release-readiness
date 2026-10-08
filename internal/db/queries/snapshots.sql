-- name: CreateSnapshot :execlastid
INSERT INTO snapshots (application, name, created_at)
VALUES (?, ?, ?);

-- name: SnapshotExistsByName :one
SELECT COUNT(*) FROM snapshots WHERE name = ?;

-- name: GetSnapshotRow :one
SELECT id, application, name, created_at
FROM snapshots WHERE name = ?;

-- name: CreateSnapshotComponent :exec
INSERT INTO snapshot_components (snapshot_id, component, git_sha, image_url, git_url)
VALUES (?, ?, ?, ?, ?);

-- name: ListSnapshotComponents :many
SELECT id, snapshot_id, component, git_sha, image_url, git_url
FROM snapshot_components
WHERE snapshot_id = ?
ORDER BY component;

-- name: ListAllSnapshots :many
SELECT id, application, name, created_at
FROM snapshots
ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?;

-- name: ListSnapshotsByApplication :many
SELECT id, application, name, created_at
FROM snapshots
WHERE application = ?
ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?;

-- name: LatestSnapshotPerApplication :many
SELECT s.id, s.application, s.name, s.created_at, CAST(counts.cnt AS INTEGER) AS cnt
FROM snapshots s
JOIN (
    SELECT application, COUNT(*) AS cnt
    FROM snapshots
    GROUP BY application
) counts ON s.application = counts.application
WHERE s.id = (
    SELECT id FROM snapshots latest
    WHERE latest.application = s.application
    ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1
)
ORDER BY s.application;

-- name: GetSnapshotByID :one
SELECT id, application, name, created_at
FROM snapshots WHERE id = ?;
