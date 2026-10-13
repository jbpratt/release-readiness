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
-- than the retry cutoff. Images held by a stage-staged image Snapshot come
-- first, as those are the builds a release ships; then images held by an
-- application's newest Snapshot, so every release gets a newest build on a
-- fresh DB before the backlog drains; then newest Snapshot first. first_seen
-- bounds the build date: an image is built before any Snapshot holds it.
-- applications lists every application holding the image, comma-separated.
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
LEFT JOIN staged_snapshots ss ON ss.name = s.name AND ss.kind = 'image' AND ss.env = 'stage'
WHERE instr(sc.image_url, '@sha256:') > 0
  AND (a.digest IS NULL OR (a.state = 'unresolved' AND a.checked_at < ?))
GROUP BY sc.image_url
ORDER BY MAX(ss.name IS NOT NULL) DESC,
         MAX(n.newest IS NOT NULL) DESC,
         last_seen DESC
LIMIT ?;

-- name: ListResolvedArtBuilds :many
SELECT digest, state, nvr, record_id, checked_at
FROM art_builds
WHERE state = 'resolved' AND digest IN (sqlc.slice('digests'));

-- name: UpsertArtHistoryBuild :exec
INSERT INTO art_history_records (record_id, group_name, build_type, nvr, digest, pullspec, commitish, start_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(record_id) DO UPDATE SET
    group_name = excluded.group_name,
    build_type = excluded.build_type,
    nvr = excluded.nvr,
    digest = excluded.digest,
    pullspec = excluded.pullspec,
    commitish = excluded.commitish,
    start_time = excluded.start_time;

-- name: ListArtHistoryImages :many
-- The bundle and image builds among digests, by digest, then NVR.
SELECT digest, nvr, commitish, record_id
FROM art_history_records
WHERE build_type IN ('bundle', 'image') AND digest IN (sqlc.slice('digests'))
ORDER BY digest, nvr;

-- name: HasArtHistoryRecord :one
SELECT EXISTS (SELECT 1 FROM art_history_records WHERE record_id = ?);

-- name: HasArtHistoryNVR :one
SELECT EXISTS (SELECT 1 FROM art_history_records WHERE group_name = ? AND build_type = ? AND nvr = ?);

-- name: HasArtHistoryDigest :one
SELECT EXISTS (SELECT 1 FROM art_history_records WHERE build_type = ? AND digest = ?);

-- name: ListArtHistoryScanDays :many
SELECT day FROM art_history_record_days WHERE group_name = ? AND build_type = ?;

-- name: UpsertArtHistoryScanDay :exec
INSERT INTO art_history_record_days (group_name, build_type, day, checked_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(group_name, build_type, day) DO UPDATE SET checked_at = excluded.checked_at;

-- name: ListSnapshotApplications :many
SELECT DISTINCT application FROM snapshots ORDER BY application;

-- name: ListArtHistoryFBCs :many
-- group's successful FBC builds, newest NVR first, each with the channel,
-- head CSV and previous entry's version its render found and the NVR of its
-- head bundle's ART build, '' for none.
SELECT f.record_id, f.nvr, f.pullspec,
       CAST(COALESCE(r.channel, '') AS TEXT) AS channel,
       CAST(COALESCE(r.head_csv, '') AS TEXT) AS head_csv,
       CAST(COALESCE(r.previous_version, '') AS TEXT) AS previous_version,
       CAST(COALESCE(b.nvr, '') AS TEXT) AS head_bundle_nvr
FROM art_history_records f
LEFT JOIN fbc_renders r ON r.state = 'rendered' AND r.digest = f.digest
LEFT JOIN art_history_records b ON b.build_type = 'bundle'
  AND b.digest = substr(r.head_bundle_pullspec, instr(r.head_bundle_pullspec, '@') + 1)
WHERE f.group_name = ? AND f.build_type = 'fbc'
ORDER BY f.nvr DESC, f.record_id;
