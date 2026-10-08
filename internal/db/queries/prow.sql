-- name: UpsertProwRun :exec
INSERT INTO prow_runs (job_name, build_id, kind, release_version, state, started_at, completed_at, prow_url, artifact_state, catalog_ref, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_name, build_id) DO UPDATE SET
    kind=excluded.kind,
    release_version=excluded.release_version,
    state=excluded.state,
    started_at=excluded.started_at,
    completed_at=excluded.completed_at,
    prow_url=excluded.prow_url,
    artifact_state=excluded.artifact_state,
    catalog_ref=excluded.catalog_ref,
    fetched_at=excluded.fetched_at;

-- name: DeleteProwRunImages :exec
DELETE FROM prow_run_images WHERE job_name = ? AND build_id = ?;

-- name: InsertProwRunImage :exec
INSERT INTO prow_run_images (job_name, build_id, role, source, requested_ref, digest, image_id)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListFinishedProwBuildIDs :many
SELECT build_id FROM prow_runs WHERE job_name = ? AND completed_at != '';

-- name: ListProwRunsByRelease :many
SELECT job_name, build_id, kind, release_version, state, started_at, completed_at, prow_url, artifact_state, catalog_ref, fetched_at
FROM prow_runs
WHERE release_version = ?
ORDER BY started_at DESC, build_id DESC LIMIT ? OFFSET ?;

-- name: ListProwRunsByDigest :many
SELECT job_name, build_id, kind, release_version, state, started_at, completed_at, prow_url, artifact_state, catalog_ref, fetched_at
FROM prow_runs r
WHERE EXISTS (
    SELECT 1 FROM prow_run_images i
    WHERE i.job_name = r.job_name AND i.build_id = r.build_id AND i.digest = ?
)
ORDER BY started_at DESC, build_id DESC LIMIT ? OFFSET ?;

-- name: ListProwRunImages :many
SELECT role, source, requested_ref, digest, image_id
FROM prow_run_images
WHERE job_name = ? AND build_id = ?
ORDER BY id;

-- name: UpsertProwSync :exec
INSERT INTO prow_syncs (job_name, release_version, interval_seconds, last_successful_sync)
VALUES (?, ?, ?, ?)
ON CONFLICT(job_name) DO UPDATE SET
    release_version=excluded.release_version,
    interval_seconds=excluded.interval_seconds,
    last_successful_sync=excluded.last_successful_sync;

-- name: ListProwSyncs :many
SELECT job_name, release_version, interval_seconds, last_successful_sync FROM prow_syncs;
