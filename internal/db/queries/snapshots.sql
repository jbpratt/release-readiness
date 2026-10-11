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

-- name: NewestStreamSnapshot :one
-- ART's assembly Snapshots (staged_snapshots) and its fbc-ri-* re-releases of
-- a bundle's related images are not stream builds.
SELECT name
FROM snapshots s
WHERE application = ? AND name NOT LIKE 'fbc-ri-%'
  AND NOT EXISTS (SELECT 1 FROM staged_snapshots ss WHERE ss.name = s.name)
ORDER BY created_at DESC, name DESC
LIMIT 1;

-- name: ListStreamSnapshotsWithDigest :many
-- The stream builds, as NewestStreamSnapshot filters them, holding an image
-- of the digest, newest first.
SELECT name
FROM snapshots s
WHERE application = sqlc.arg(application) AND name NOT LIKE 'fbc-ri-%'
  AND NOT EXISTS (SELECT 1 FROM staged_snapshots ss WHERE ss.name = s.name)
  AND EXISTS (SELECT 1 FROM snapshot_components sc
              WHERE sc.snapshot_id = s.id
                AND substr(sc.image_url, instr(sc.image_url, '@') + 1) = CAST(sqlc.arg(digest) AS TEXT))
ORDER BY created_at DESC, name DESC;
