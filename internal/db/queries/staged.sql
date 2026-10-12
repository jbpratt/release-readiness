-- name: UpsertStagedSnapshot :exec
INSERT INTO staged_snapshots (name, assembly, kind, env)
VALUES (?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    assembly=excluded.assembly,
    kind=excluded.kind,
    env=excluded.env;

-- name: ListSuccessfulStageReleases :many
SELECT kr.release_plan, kr.completion_time, s.name AS snapshot_name
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN konflux_releases kr ON kr.snapshot = s.name AND kr.application = s.application
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'image' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
  AND kr.released_status = 'True' AND kr.released_reason = 'Succeeded' AND kr.completion_time != ''
ORDER BY kr.completion_time DESC, kr.created_at DESC, kr.name DESC;

-- name: LatestProdImageRelease :one
SELECT id, name, application, snapshot, release_plan, released_status, released_reason, failed_task, failed_step, created_at, completion_time
FROM konflux_releases
WHERE snapshot IN (SELECT name FROM staged_snapshots WHERE assembly = ? AND kind = 'image' AND env = 'prod')
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListStagedFBCCatalogs :many
-- The catalog images of the application's FBC Snapshots ART staged for stage
-- for assembly, each with the NVR of its digest's resolved ART build, or ''.
SELECT s.name AS snapshot_name, sc.component, sc.image_url, CAST(COALESCE(a.nvr, '') AS TEXT) AS nvr
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN snapshot_components sc ON sc.snapshot_id = s.id
LEFT JOIN art_builds a ON a.state = 'resolved'
  AND a.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'fbc' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
ORDER BY s.name, sc.component;

-- name: ListStagedFBCMembers :many
-- The catalog images of the application's FBC Snapshots ART staged for stage
-- for assembly, newest Snapshot first, each with its NVR: that of its ART
-- build in build history, else of its resolved ART build, else its rendered
-- label, '' for none, and that build's record ID; the channel and head CSV
-- its render found; and the NVR of its head bundle's ART build, '' for none.
SELECT s.name AS snapshot_name, sc.image_url,
       CAST(COALESCE(h.nvr, a.nvr, NULLIF(r.label_nvr, ''), '') AS TEXT) AS nvr,
       CAST(COALESCE(h.record_id, a.record_id, '') AS TEXT) AS record_id,
       CAST(COALESCE(r.channel, '') AS TEXT) AS channel,
       CAST(COALESCE(r.head_csv, '') AS TEXT) AS head_csv,
       CAST(COALESCE(b.nvr, '') AS TEXT) AS head_bundle_nvr
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN snapshot_components sc ON sc.snapshot_id = s.id
LEFT JOIN art_history_builds h ON h.build_type = 'fbc'
  AND h.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
LEFT JOIN art_builds a ON a.state = 'resolved'
  AND a.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
LEFT JOIN fbc_render_cache r ON r.state = 'rendered'
  AND r.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
LEFT JOIN art_history_builds b ON b.build_type = 'bundle'
  AND b.digest = substr(r.head_bundle_pullspec, instr(r.head_bundle_pullspec, '@') + 1)
WHERE ss.assembly = sqlc.arg(assembly) AND ss.kind = 'fbc' AND ss.env = 'stage'
  AND s.application = sqlc.arg(application)
ORDER BY s.created_at DESC, s.name DESC, sc.component;
