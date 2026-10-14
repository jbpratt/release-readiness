-- name: ListSourcesByDigest :many
SELECT s.name, s.source_repo, h.commitish
FROM art_history_records h
JOIN art_source_repos s ON s.record_id = h.record_id
WHERE h.digest = ? AND s.source_repo != ''
ORDER BY h.nvr, h.record_id;

-- name: GetDigestSourceState :one
SELECT CAST(COUNT(*) > 0 AS BOOLEAN) AS has_record,
       CAST(COALESCE(SUM(s.record_id IS NULL), 0) > 0 AS BOOLEAN) AS has_unchecked
FROM art_history_records h
LEFT JOIN art_source_repos s ON s.record_id = h.record_id
WHERE h.digest = ?;

-- name: GetReconcileBaseline :one
SELECT fbc_digest
FROM reconcile_baselines
WHERE stream = ? AND operator = ? AND ocp = ?;

-- name: InsertReconcileBaseline :exec
INSERT INTO reconcile_baselines (stream, operator, ocp, fbc_digest)
VALUES (?, ?, ?, ?)
ON CONFLICT(stream, operator, ocp) DO NOTHING;

-- name: UpsertReconcileAction :exec
-- The reconciler's rows (no attempts) replace only its other unattempted,
-- unproposed rows; the writer's move a proposed or error row on, never back
-- to an earlier attempt.
INSERT INTO reconcile_actions (
    fbc_digest, issue_key, release_name, assembly, fbc_nvr, evidence_json,
    desired_target_version, status, skip_reason, error, version_step,
    transition_step, comment_step, attempts, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(fbc_digest, issue_key, release_name) DO UPDATE SET
    assembly = excluded.assembly,
    fbc_nvr = excluded.fbc_nvr,
    evidence_json = excluded.evidence_json,
    desired_target_version = excluded.desired_target_version,
    status = excluded.status,
    skip_reason = excluded.skip_reason,
    error = excluded.error,
    version_step = excluded.version_step,
    transition_step = excluded.transition_step,
    comment_step = excluded.comment_step,
    attempts = excluded.attempts,
    updated_at = excluded.updated_at
WHERE (excluded.attempts = 0 AND reconcile_actions.attempts = 0 AND reconcile_actions.status != 'proposed')
   OR (excluded.attempts >= reconcile_actions.attempts AND excluded.attempts > 0
       AND reconcile_actions.status IN ('proposed', 'error'));

-- name: ListPendingReconcileActions :many
SELECT * FROM reconcile_actions
WHERE status = 'proposed' OR (status = 'error' AND attempts < ?)
ORDER BY updated_at, release_name, fbc_digest, issue_key;

-- name: FirstIncludedReconcileAction :one
-- prefix is the stream and a dot, so 3.1 does not match 3.18.x.
SELECT * FROM reconcile_actions
WHERE issue_key = sqlc.arg(issue_key)
  AND substr(assembly, 1, length(CAST(sqlc.arg(prefix) AS TEXT))) = CAST(sqlc.arg(prefix) AS TEXT)
  AND status IN ('proposed', 'applied', 'error')
ORDER BY updated_at, release_name
LIMIT 1;

-- name: ListStageFBCs :many
-- Each catalog of an FBC Snapshot ART staged for stage whose Release
-- succeeded, across all assemblies, oldest completion first, with the state
-- of its render, empty before the first, and the NVR of its ART FBC build, else
-- its rendered label.
SELECT kr.name AS release_name, ss.assembly, kr.completion_time,
       CAST(substr(sc.image_url, instr(sc.image_url, '@') + 1) AS TEXT) AS fbc_digest,
       CAST(COALESCE(r.state, '') AS TEXT) AS render_state,
       CAST(COALESCE((SELECT h.nvr FROM art_history_records h
                      WHERE h.build_type = 'fbc' AND h.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
                      ORDER BY h.start_time DESC, h.nvr DESC LIMIT 1), r.label_nvr, '') AS TEXT) AS fbc_nvr
FROM staged_snapshots ss
JOIN snapshots s ON s.name = ss.name
JOIN snapshot_components sc ON sc.snapshot_id = s.id
JOIN konflux_releases kr ON kr.snapshot = s.name AND kr.application = s.application
LEFT JOIN fbc_renders r ON r.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
WHERE ss.kind = 'fbc' AND ss.env = 'stage' AND instr(sc.image_url, '@sha256:') > 0
  AND kr.released_status = 'True' AND kr.released_reason = 'Succeeded' AND kr.completion_time != ''
ORDER BY kr.completion_time, kr.name, fbc_digest;

-- name: ListFBCMemberDigests :many
-- The digests of a rendered FBC's head bundle and its related images; a
-- related image without one is returned whole.
SELECT CAST(substr(image, instr(image, '@') + 1) AS TEXT) AS digest
FROM fbc_related_images
WHERE fbc_related_images.digest = sqlc.arg(fbc_digest) AND role = 'head'
UNION
SELECT CAST(substr(head_bundle_pullspec, instr(head_bundle_pullspec, '@') + 1) AS TEXT)
FROM fbc_renders
WHERE fbc_renders.digest = sqlc.arg(fbc_digest) AND state = 'rendered' AND head_bundle_pullspec != ''
ORDER BY 1;

-- name: ReconcileRunDone :one
SELECT CAST(EXISTS (SELECT 1 FROM reconcile_runs WHERE release_name = ? AND fbc_digest = ?) AS BOOLEAN);

-- name: InsertReconcileRun :exec
INSERT INTO reconcile_runs (release_name, fbc_digest)
VALUES (?, ?)
ON CONFLICT(release_name, fbc_digest) DO NOTHING;
