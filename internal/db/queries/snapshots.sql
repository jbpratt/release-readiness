-- name: CreateSnapshot :execlastid
INSERT INTO snapshots (application, name, created_at)
VALUES (?, ?, ?);

-- name: SnapshotExistsByName :one
SELECT COUNT(*) FROM snapshots WHERE name = ?;

-- name: GetSnapshotRow :one
SELECT id, application, name, created_at
FROM snapshots WHERE name = ?;

-- name: CreateSnapshotComponent :exec
INSERT INTO snapshot_components (snapshot_id, component, image_url)
VALUES (?, ?, ?);

-- name: ListSnapshotComponents :many
SELECT id, snapshot_id, component, image_url
FROM snapshot_components
WHERE snapshot_id = ?
ORDER BY component;

-- name: ListComponentCandidates :many
SELECT sc.id, sc.component, sc.image_url,
       s.id AS snapshot_id, s.application, s.created_at
FROM snapshot_components sc
JOIN snapshots s ON s.id = sc.snapshot_id
WHERE s.application IN (sqlc.slice('applications'));

-- name: ListReleaseSnapshots :many
-- Snapshots of the given applications, plus Snapshots a Release names that are
-- no longer stored (missing = 1). quay-images-base is shared across versions,
-- so its Snapshots count only with a component matching the LIKE pattern.
WITH candidates AS (
    SELECT s.name, s.application, s.created_at,
           (SELECT COUNT(*) FROM snapshot_components sc WHERE sc.snapshot_id = s.id) AS component_count,
           0 AS missing
    FROM snapshots s
    WHERE s.application IN (sqlc.slice('applications'))
      AND (s.application != ?
           OR EXISTS (SELECT 1 FROM snapshot_components sc
                      WHERE sc.snapshot_id = s.id AND sc.component LIKE ?))
      AND (? = 0 OR EXISTS (SELECT 1 FROM konflux_releases r
                            WHERE r.snapshot = s.name AND r.application = s.application))
    UNION ALL
    SELECT r.snapshot, r.application, MIN(r.created_at), 0, 1
    FROM konflux_releases r
    WHERE r.application IN (sqlc.slice('missing_applications'))
      AND r.snapshot != ''
      AND NOT EXISTS (SELECT 1 FROM snapshots s WHERE s.name = r.snapshot)
    GROUP BY r.application, r.snapshot
)
SELECT c.name, c.application, c.created_at, c.component_count, c.missing,
       COALESCE(ss.kind, '') AS art_kind
FROM candidates c
LEFT JOIN staged_snapshots ss ON ss.name = c.name
ORDER BY c.created_at DESC, c.name DESC
LIMIT ? OFFSET ?;
