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

-- name: ListStreamBuildImages :many
-- The component images of the application's stream builds, newest first, each
-- with the NVR of its digest's resolved ART build, or ''. ART's assembly
-- Snapshots (staged_snapshots) and its fbc-ri-* re-releases of a bundle's
-- related images are not stream builds.
SELECT s.name, s.created_at, sc.component, sc.image_url, CAST(COALESCE(a.nvr, '') AS TEXT) AS nvr
FROM snapshots s
JOIN snapshot_components sc ON sc.snapshot_id = s.id
LEFT JOIN art_builds a ON a.state = 'resolved'
  AND a.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
WHERE s.application = ? AND s.name NOT LIKE 'fbc-ri-%'
  AND NOT EXISTS (SELECT 1 FROM staged_snapshots ss WHERE ss.name = s.name)
ORDER BY s.created_at DESC, s.name DESC, sc.component;
