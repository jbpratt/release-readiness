-- name: UpsertFBCRender :exec
-- A rendered row is final, so only a failed one is replaced.
INSERT INTO fbc_render_cache (digest, state, label_nvr, package_name, channel, head_csv, head_bundle_pullspec, replaces, skip_range, error, rendered_at, retry_after)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    label_nvr = excluded.label_nvr,
    package_name = excluded.package_name,
    channel = excluded.channel,
    head_csv = excluded.head_csv,
    head_bundle_pullspec = excluded.head_bundle_pullspec,
    replaces = excluded.replaces,
    skip_range = excluded.skip_range,
    error = excluded.error,
    rendered_at = excluded.rendered_at,
    retry_after = excluded.retry_after
WHERE fbc_render_cache.state = 'failed';

-- name: ListFBCRenderSources :many
-- FBC images to render, ART's successful FBC builds and the catalogs of FBC
-- Snapshots ART staged, each with the state and retry time of its render, ''
-- without one. nvr is '' for a staged catalog; created is when ART started
-- the build or Konflux created the Snapshot.
SELECT src.pullspec, src.nvr, src.created,
       CAST(COALESCE(r.state, '') AS TEXT) AS state,
       CAST(COALESCE(r.retry_after, '') AS TEXT) AS retry_after
FROM (
    SELECT pullspec, nvr, start_time AS created FROM art_history_builds WHERE build_type = 'fbc'
    UNION
    SELECT sc.image_url, '', s.created_at
    FROM staged_snapshots ss
    JOIN snapshots s ON s.name = ss.name
    JOIN snapshot_components sc ON sc.snapshot_id = s.id
    WHERE ss.kind = 'fbc' AND instr(sc.image_url, '@sha256:') > 0
) src
LEFT JOIN fbc_render_cache r ON r.digest = substr(src.pullspec, instr(src.pullspec, '@') + 1);

-- name: ListUnjoinedFBCRenders :many
-- Rendered FBC images whose head bundle image has no stored ART bundle build.
SELECT r.digest, r.label_nvr, r.channel, r.head_bundle_pullspec
FROM fbc_render_cache r
WHERE r.state = 'rendered'
  AND NOT EXISTS (
    SELECT 1 FROM art_history_builds b
    WHERE b.build_type = 'bundle'
      AND b.digest = substr(r.head_bundle_pullspec, instr(r.head_bundle_pullspec, '@') + 1));
