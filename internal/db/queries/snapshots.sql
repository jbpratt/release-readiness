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

-- name: ListComponentCandidates :many
SELECT sc.id, sc.component, sc.git_sha, sc.image_url, sc.git_url,
       s.id AS snapshot_id, s.application, s.name AS snapshot, s.created_at
FROM snapshot_components sc
JOIN snapshots s ON s.id = sc.snapshot_id
WHERE s.application IN (sqlc.slice('applications'));

-- name: ListReleaseSnapshots :many
SELECT id, application, name, created_at
FROM snapshots s
WHERE s.application IN (sqlc.slice('applications'))
  AND (s.application != ?
       OR EXISTS (SELECT 1 FROM snapshot_components sc
                  WHERE sc.snapshot_id = s.id AND sc.component LIKE ?))
ORDER BY s.created_at DESC, s.id DESC
LIMIT ? OFFSET ?;

-- name: GetSnapshotByID :one
SELECT id, application, name, created_at
FROM snapshots WHERE id = ?;
