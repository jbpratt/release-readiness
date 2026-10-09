-- name: UpsertEvidenceScan :exec
INSERT INTO build_evidence_scans (snapshot_name, component, image_digest, upstream_repo, base_sha, head_sha, state, reason, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(snapshot_name, component, image_digest) DO UPDATE SET
    upstream_repo = excluded.upstream_repo,
    base_sha = excluded.base_sha,
    head_sha = excluded.head_sha,
    state = excluded.state,
    reason = excluded.reason,
    checked_at = excluded.checked_at;

-- name: DeleteScanEvidence :exec
DELETE FROM ticket_evidence WHERE snapshot_name = ? AND component = ? AND image_digest = ?;

-- name: InsertTicketEvidence :exec
INSERT INTO ticket_evidence (snapshot_name, component, image_digest, ticket_key, commit_sha, commit_url, pr_url, source, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListEvidenceScans :many
SELECT snapshot_name, component, image_digest, upstream_repo, base_sha, head_sha, state, reason, checked_at
FROM build_evidence_scans
WHERE snapshot_name = ?
ORDER BY component, image_digest;

-- name: ListSnapshotTicketEvidence :many
SELECT snapshot_name, component, image_digest, ticket_key, commit_sha, commit_url, pr_url, source, checked_at
FROM ticket_evidence
WHERE snapshot_name = ?
ORDER BY component, image_digest, ticket_key, commit_sha;

-- name: GetCompleteScanByRange :one
SELECT snapshot_name, component, image_digest, upstream_repo, base_sha, head_sha, state, reason, checked_at
FROM build_evidence_scans
WHERE upstream_repo = ? AND base_sha = ? AND head_sha = ? AND state = 'complete'
ORDER BY checked_at DESC
LIMIT 1;
