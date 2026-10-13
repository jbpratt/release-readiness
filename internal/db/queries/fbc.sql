-- name: UpsertFBCRender :exec
-- A rendered row is final, so only a failed one is replaced.
INSERT INTO fbc_renders (digest, state, label_nvr, channel, head_csv, head_bundle_pullspec, previous_version, error, rendered_at, retry_after)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    label_nvr = excluded.label_nvr,
    channel = excluded.channel,
    head_csv = excluded.head_csv,
    head_bundle_pullspec = excluded.head_bundle_pullspec,
    previous_version = excluded.previous_version,
    error = excluded.error,
    rendered_at = excluded.rendered_at,
    retry_after = excluded.retry_after
WHERE fbc_renders.state = 'failed';

-- name: InsertFBCRelatedImage :exec
-- A rendered image's related images never change, so a stored one stays.
INSERT INTO fbc_related_images (digest, role, name, image) VALUES (?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: ListFBCRelatedImages :many
SELECT role, name, image FROM fbc_related_images WHERE digest = ? ORDER BY role, name, image;

-- name: ListFBCRenderSources :many
-- FBC images to render, ART's successful FBC builds and the catalogs of FBC
-- Snapshots ART staged, each with the state and retry time of its render, ''
-- without one. nvr is '' for a staged catalog; created is when ART started
-- the build or Konflux created the Snapshot.
SELECT src.pullspec, src.nvr, src.created,
       CAST(COALESCE(r.state, '') AS TEXT) AS state,
       CAST(COALESCE(r.retry_after, '') AS TEXT) AS retry_after
FROM (
    SELECT pullspec, nvr, start_time AS created FROM art_history_records WHERE build_type = 'fbc'
    UNION
    SELECT sc.image_url, '', s.created_at
    FROM staged_snapshots ss
    JOIN snapshots s ON s.name = ss.name
    JOIN snapshot_components sc ON sc.snapshot_id = s.id
    WHERE ss.kind = 'fbc' AND instr(sc.image_url, '@sha256:') > 0
) src
LEFT JOIN fbc_renders r ON r.digest = substr(src.pullspec, instr(src.pullspec, '@') + 1);

-- name: ListUnjoinedFBCRenders :many
-- Rendered FBC images whose head bundle image has no stored ART bundle build.
SELECT r.digest, r.label_nvr, r.channel, r.head_bundle_pullspec
FROM fbc_renders r
WHERE r.state = 'rendered'
  AND NOT EXISTS (
    SELECT 1 FROM art_history_records b
    WHERE b.build_type = 'bundle'
      AND b.digest = substr(r.head_bundle_pullspec, instr(r.head_bundle_pullspec, '@') + 1));
