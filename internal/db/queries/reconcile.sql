-- name: GetSourceByRecord :one
SELECT h.record_id, s.name, h.nvr, h.digest, s.source_repo, h.commitish
FROM art_history_records h
JOIN art_source_repos s ON s.record_id = h.record_id
WHERE h.record_id = ? AND s.source_repo != '';

-- name: ListSourcesByDigest :many
SELECT h.record_id, s.name, h.nvr, h.digest, s.source_repo, h.commitish
FROM art_history_records h
JOIN art_source_repos s ON s.record_id = h.record_id
WHERE h.digest = ? AND s.source_repo != ''
ORDER BY h.nvr, h.record_id;

-- name: GetReconcileBaseline :one
SELECT stream, operator, ocp, fbc_digest, fbc_nvr
FROM reconcile_baselines
WHERE stream = ? AND operator = ? AND ocp = ?;

-- name: InsertReconcileBaseline :exec
INSERT INTO reconcile_baselines (stream, operator, ocp, fbc_digest, fbc_nvr, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(stream, operator, ocp) DO NOTHING;
