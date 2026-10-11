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

-- name: DeleteProwInstallFailure :exec
DELETE FROM prow_install_failures WHERE job_name = ? AND build_id = ?;

-- name: InsertProwInstallFailure :exec
INSERT INTO prow_install_failures (job_name, build_id) VALUES (?, ?);

-- name: ListFinishedProwBuildIDs :many
-- A finished run still "missing" was stored by a binary that did not read
-- olm-csvs.yaml, so it is read once more.
SELECT build_id FROM prow_runs WHERE job_name = ? AND completed_at != '' AND artifact_state != 'missing';

-- name: ListProwRunsByApplication :many
SELECT r.job_name, r.build_id, r.kind, r.application, r.state, r.started_at, r.completed_at, r.prow_url, r.artifact_state, r.catalog_ref, r.fetched_at,
       f.build_id IS NOT NULL AS install_failed
FROM prow_runs r
LEFT JOIN prow_install_failures f ON f.job_name = r.job_name AND f.build_id = r.build_id
WHERE r.application = ?
ORDER BY r.started_at DESC, r.build_id DESC;

-- name: ListProwRunImages :many
SELECT role, digest
FROM prow_run_images
WHERE job_name = ? AND build_id = ?
ORDER BY id;
