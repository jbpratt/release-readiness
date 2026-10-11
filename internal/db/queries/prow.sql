-- name: UpsertProwRun :exec
INSERT INTO prow_runs (job_name, build_id, kind, application, state, started_at, completed_at, prow_url, artifact_state, catalog_ref, fetched_at)
VALUES (?, ?, 'periodic', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_name, build_id) DO UPDATE SET
    application=excluded.application,
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
INSERT INTO prow_run_images (job_name, build_id, role, digest)
VALUES (?, ?, ?, ?);

-- name: ListFinishedProwBuildIDs :many
-- A finished run still "missing" was stored by a binary that did not read
-- olm-csvs.yaml, so it is read once more.
SELECT build_id FROM prow_runs WHERE job_name = ? AND completed_at != '' AND artifact_state != 'missing';

-- name: ListProwRunsByApplication :many
SELECT job_name, build_id, kind, application, state, started_at, completed_at, prow_url, artifact_state, catalog_ref, fetched_at
FROM prow_runs
WHERE application = ?
ORDER BY started_at DESC, build_id DESC;

-- name: ListProwRunImages :many
SELECT role, digest
FROM prow_run_images
WHERE job_name = ? AND build_id = ?
ORDER BY id;
