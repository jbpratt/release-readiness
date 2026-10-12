-- name: UpsertArtBuild :exec
INSERT INTO art_builds (digest, state, nvr, record_id, checked_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    state = excluded.state,
    nvr = excluded.nvr,
    record_id = excluded.record_id,
    checked_at = excluded.checked_at;

-- name: ListArtBuildCandidates :many
-- Stored component images with no ART record yet, or whose last miss is older
-- than the retry cutoff. Images held by an application's newest Snapshot come
-- first, so every release gets a newest build on a fresh DB before the backlog
-- drains; then newest Snapshot first. first_seen bounds the build date: an image
-- is built before any Snapshot holds it. applications lists every application
-- holding the image, comma-separated.
SELECT sc.image_url,
       CAST(GROUP_CONCAT(DISTINCT s.application) AS TEXT) AS applications,
       CAST(MIN(sc.component) AS TEXT) AS component,
       CAST(MIN(s.created_at) AS TEXT) AS first_seen,
       CAST(MAX(s.created_at) AS TEXT) AS last_seen
FROM snapshot_components sc
JOIN snapshots s ON s.id = sc.snapshot_id
LEFT JOIN art_builds a ON a.digest = substr(sc.image_url, instr(sc.image_url, '@') + 1)
LEFT JOIN (SELECT application, MAX(created_at) AS newest FROM snapshots GROUP BY application) n
  ON n.application = s.application AND n.newest = s.created_at
WHERE instr(sc.image_url, '@sha256:') > 0
  AND (a.digest IS NULL OR (a.state = 'unresolved' AND a.checked_at < ?))
GROUP BY sc.image_url
ORDER BY MAX(n.newest IS NOT NULL) DESC,
         last_seen DESC
LIMIT ?;

-- name: ListResolvedArtBuilds :many
SELECT digest, state, nvr, record_id, checked_at
FROM art_builds
WHERE state = 'resolved' AND digest IN (sqlc.slice('digests'));

-- name: UpsertArtFBCRecord :exec
INSERT INTO art_fbc_records (digest, pullspec, bundle_nvrs, end_time)
VALUES (?, ?, ?, ?)
ON CONFLICT(digest) DO UPDATE SET
    pullspec = excluded.pullspec,
    bundle_nvrs = excluded.bundle_nvrs,
    end_time = excluded.end_time;

-- name: GetArtFBCBundleNVRs :one
SELECT bundle_nvrs FROM art_fbc_records WHERE digest = ?;

-- name: ListUnreadArtFBCImages :many
-- Resolved images whose NVR may be an FBC image's, which the caller parses,
-- with no FBC record yet.
SELECT a.digest, a.nvr, a.record_id
FROM art_builds a
WHERE a.state = 'resolved' AND a.nvr LIKE '%-fbc-%'
  AND NOT EXISTS (SELECT 1 FROM art_fbc_records f WHERE f.digest = a.digest);

-- name: ListArtFBCImages :many
-- Resolved images whose NVR may be an FBC image's, which the caller parses,
-- each with its FBC record, NULL until read.
SELECT a.digest, a.nvr, f.pullspec, f.bundle_nvrs, f.end_time
FROM art_builds a
LEFT JOIN art_fbc_records f ON f.digest = a.digest
WHERE a.state = 'resolved' AND a.nvr LIKE '%-fbc-%';
