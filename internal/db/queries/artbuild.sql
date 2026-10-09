-- name: UpsertArtBuild :exec
INSERT INTO art_builds (digest, state, nvr, record_id, upstream_repo, upstream_sha, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    nvr = excluded.nvr,
    record_id = excluded.record_id,
    upstream_repo = excluded.upstream_repo,
    upstream_sha = excluded.upstream_sha,
    checked_at = excluded.checked_at;

-- name: ListArtBuildCandidates :many
-- Stored component images with no ART record yet, or whose last miss is older
-- than the retry cutoff, newest Snapshot first. first_seen bounds the build
-- date: an image is built before any Snapshot holds it. applications lists every
-- application holding the image, comma-separated.
SELECT sc.image_url,
       CAST(GROUP_CONCAT(DISTINCT s.application) AS TEXT) AS applications,
       CAST(MIN(sc.component) AS TEXT) AS component,
       CAST(MIN(s.created_at) AS TEXT) AS first_seen,
       CAST(MAX(s.created_at) AS TEXT) AS last_seen
FROM snapshot_components sc
JOIN snapshots s ON s.id = sc.snapshot_id
LEFT JOIN art_builds a ON a.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
WHERE instr(sc.image_url, '@sha256:') > 0
  AND (a.digest IS NULL OR (a.state = 'unresolved' AND a.checked_at < ?))
GROUP BY sc.image_url
ORDER BY last_seen DESC
LIMIT ?;

-- name: ListResolvedArtBuilds :many
SELECT digest, state, nvr, record_id, upstream_repo, upstream_sha, checked_at
FROM art_builds
WHERE state = 'resolved' AND digest IN (sqlc.slice('digests'));

-- name: ListActiveApplications :many
SELECT DISTINCT konflux_application
FROM release_versions
WHERE released = 0 AND archived = 0 AND konflux_application != ''
ORDER BY konflux_application;

-- name: DeleteArtPendingBuilds :exec
DELETE FROM art_pending_builds WHERE group_name = ?;

-- name: InsertArtPendingBuild :exec
INSERT INTO art_pending_builds (group_name, release_version, component, nvr, record_id, upstream_sha, started_at, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListArtPendingBuilds :many
-- Pending builds of the given versions from a search no older than checked_at.
SELECT group_name, release_version, component, nvr, record_id, upstream_sha, started_at, checked_at
FROM art_pending_builds
WHERE checked_at >= ? AND release_version IN (sqlc.slice('versions'));

-- name: UpsertArtBuildAttempt :exec
INSERT INTO art_build_attempts (group_name, record_id, release_version, component, nvr, outcome, start_time, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(group_name, record_id) DO UPDATE SET
    outcome = excluded.outcome,
    start_time = excluded.start_time,
    last_seen = excluded.last_seen;

-- name: UpsertArtBuildCoverage :exec
-- A search window that starts after the stored span ends leaves a gap, so the
-- span restarts at the window.
INSERT INTO art_build_coverage (group_name, covered_from, covered_to)
VALUES (?, ?, ?)
ON CONFLICT(group_name) DO UPDATE SET
    covered_from = CASE
        WHEN excluded.covered_from <= art_build_coverage.covered_to
        THEN min(art_build_coverage.covered_from, excluded.covered_from)
        ELSE excluded.covered_from
    END,
    covered_to = excluded.covered_to;

-- name: GetArtBuildCoverage :one
SELECT covered_from FROM art_build_coverage WHERE group_name = ?;

-- name: ListArtBuildAttempts :many
-- A version's attempts inside its group's covered span, newest first. A
-- pending record whose NVR has a finished one is the same attempt.
SELECT a.component, a.nvr, a.record_id, a.outcome, a.start_time
FROM art_build_attempts a
JOIN art_build_coverage c ON c.group_name = a.group_name
WHERE a.group_name = ? AND a.release_version = ?
  AND a.start_time >= c.covered_from
  AND NOT (a.outcome = 'pending' AND EXISTS (
      SELECT 1 FROM art_build_attempts f
      WHERE f.group_name = a.group_name AND f.nvr = a.nvr AND f.outcome != 'pending'))
ORDER BY a.start_time DESC, a.record_id DESC;
