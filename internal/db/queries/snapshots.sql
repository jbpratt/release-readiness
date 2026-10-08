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
ORDER BY id DESC LIMIT ? OFFSET ?;

-- name: ListSnapshotsByApplication :many
SELECT id, application, name, created_at
FROM snapshots
WHERE application = ?
ORDER BY id DESC LIMIT ? OFFSET ?;

-- name: LatestSnapshotPerApplication :many
SELECT s.id, s.application, s.name, s.created_at, CAST(counts.cnt AS INTEGER) AS cnt
FROM snapshots s
JOIN (
    SELECT application, MAX(id) AS max_id, COUNT(*) AS cnt
    FROM snapshots
    GROUP BY application
) counts ON s.id = counts.max_id
ORDER BY s.application;

-- name: GetSnapshotByID :one
SELECT id, application, name, created_at
FROM snapshots WHERE id = ?;
