-- name: UpsertArtBuild :exec
INSERT INTO art_builds (digest, state, nvr, record_id, upstream_repo, upstream_sha, rebase_repo, rebase_sha, pipeline_url, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    nvr = excluded.nvr,
    record_id = excluded.record_id,
    upstream_repo = excluded.upstream_repo,
    upstream_sha = excluded.upstream_sha,
    rebase_repo = excluded.rebase_repo,
    rebase_sha = excluded.rebase_sha,
    pipeline_url = excluded.pipeline_url,
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
SELECT digest, state, nvr, record_id, upstream_repo, upstream_sha, rebase_repo, rebase_sha, pipeline_url, checked_at
FROM art_builds
WHERE state = 'resolved' AND digest IN (sqlc.slice('digests'));
