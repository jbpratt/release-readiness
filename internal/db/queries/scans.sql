-- name: UpsertImageScan :exec
INSERT INTO image_scans (digest, state, pipeline_run, detail_url, counts_json, reports_json, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    pipeline_run = excluded.pipeline_run,
    detail_url = excluded.detail_url,
    counts_json = excluded.counts_json,
    reports_json = excluded.reports_json,
    checked_at = excluded.checked_at;

-- name: ListImageScans :many
SELECT digest, state, pipeline_run, detail_url, counts_json, checked_at
FROM image_scans
WHERE digest IN (sqlc.slice('digests'));
