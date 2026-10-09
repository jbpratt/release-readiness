-- name: ListFBCCatalogCandidates :many
-- The quay-operator image of each FBC application's newest Snapshot holding
-- one, when its catalog is unread or its last read failed before the cutoff.
-- Snapshots ART staged share the application and component but are not the
-- stream's builds, so they are skipped.
SELECT sc.image_url
FROM snapshot_components sc
JOIN snapshots s ON s.id = sc.snapshot_id
LEFT JOIN fbc_catalogs c ON c.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
WHERE s.application LIKE 'fbc-%'
  AND sc.component = s.application || '-quay-operator'
  AND instr(sc.image_url, '@sha256:') > 0
  AND s.id = (SELECT s2.id
              FROM snapshots s2
              JOIN snapshot_components sc2 ON sc2.snapshot_id = s2.id
              WHERE s2.application = s.application AND sc2.component = sc.component
                AND NOT EXISTS (SELECT 1 FROM staged_snapshots ss WHERE ss.name = s2.name)
              ORDER BY s2.created_at DESC, s2.id DESC
              LIMIT 1)
  AND (c.digest IS NULL OR (c.state = 'failed' AND c.checked_at < ?))
ORDER BY s.created_at DESC
LIMIT ?;

-- name: UpsertFBCCatalog :exec
INSERT INTO fbc_catalogs (digest, state, checked_at)
VALUES (?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    checked_at = excluded.checked_at;

-- name: DeleteFBCCatalogBundles :exec
DELETE FROM fbc_catalog_bundles WHERE catalog_digest = ?;

-- name: InsertFBCCatalogBundle :exec
INSERT INTO fbc_catalog_bundles (catalog_digest, package, channel, bundle_name, bundle_ref, bundle_digest)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetLatestSnapshotImage :one
-- The newest Snapshot of an application holding a component, with its image,
-- skipping the Snapshots ART staged.
SELECT s.name, sc.image_url
FROM snapshots s
JOIN snapshot_components sc ON sc.snapshot_id = s.id
WHERE s.application = ? AND sc.component = ?
  AND NOT EXISTS (SELECT 1 FROM staged_snapshots ss WHERE ss.name = s.name)
ORDER BY s.created_at DESC, s.id DESC
LIMIT 1;

-- name: GetFBCCatalogState :one
SELECT state FROM fbc_catalogs WHERE digest = ?;

-- name: ListFBCCatalogBundles :many
SELECT package, channel, bundle_name, bundle_ref, bundle_digest
FROM fbc_catalog_bundles
WHERE catalog_digest = ? AND package = ? AND channel = ?
ORDER BY bundle_name;
