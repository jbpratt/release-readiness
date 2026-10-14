-- name: PruneFailedReleases :execrows
-- Keeps those of ART staged Snapshots, whose newest Release the candidate shows.
DELETE FROM konflux_releases
WHERE released_reason = 'Failed' AND created_at < sqlc.arg(cutoff)
  AND snapshot NOT IN (SELECT name FROM staged_snapshots);

-- name: PruneOrphanKonfluxReleaseArt :execrows
DELETE FROM konflux_release_art
WHERE name NOT IN (SELECT name FROM konflux_releases);

-- name: PruneSnapshots :execrows
-- Keeps ART staged Snapshots, those a Release shipped, and the newest of each
-- application. Components go by cascade.
DELETE FROM snapshots
WHERE snapshots.created_at < sqlc.arg(cutoff)
  AND name NOT IN (SELECT name FROM staged_snapshots)
  AND name NOT IN (SELECT snapshot FROM konflux_releases WHERE released_reason = 'Succeeded')
  AND snapshots.created_at < (SELECT MAX(n.created_at) FROM snapshots n WHERE n.application = snapshots.application);

-- name: PruneOrphanStagedSnapshots :execrows
DELETE FROM staged_snapshots
WHERE name NOT IN (SELECT name FROM snapshots);

-- name: PruneProwRuns :execrows
-- Keeps the newest run of each job and runs that tested a stored catalog.
-- Images and install failures go by cascade.
DELETE FROM prow_runs
WHERE prow_runs.started_at < sqlc.arg(cutoff)
  AND prow_runs.started_at < (SELECT MAX(n.started_at) FROM prow_runs n WHERE n.job_name = prow_runs.job_name)
  AND (catalog_ref = '' OR substr(catalog_ref, instr(catalog_ref, '@') + 1) NOT IN (
      SELECT substr(image_url, instr(image_url, '@') + 1) FROM snapshot_components));

-- name: PruneArtBuilds :execrows
DELETE FROM art_builds
WHERE checked_at < sqlc.arg(cutoff)
  AND digest NOT IN (
      SELECT substr(image_url, instr(image_url, '@') + 1) FROM snapshot_components
      UNION SELECT digest FROM art_history_records
      UNION SELECT substr(catalog_ref, instr(catalog_ref, '@') + 1) FROM prow_runs
      UNION SELECT digest FROM prow_run_images);

-- name: PruneImageScans :execrows
DELETE FROM image_scans
WHERE checked_at < sqlc.arg(cutoff)
  AND digest NOT IN (
      SELECT substr(image_url, instr(image_url, '@') + 1) FROM snapshot_components
      UNION SELECT digest FROM art_history_records
      UNION SELECT substr(catalog_ref, instr(catalog_ref, '@') + 1) FROM prow_runs
      UNION SELECT digest FROM prow_run_images);

-- name: PruneFBCRenders :execrows
DELETE FROM fbc_renders
WHERE rendered_at < sqlc.arg(cutoff)
  AND digest NOT IN (
      SELECT substr(image_url, instr(image_url, '@') + 1) FROM snapshot_components
      UNION SELECT digest FROM art_history_records
      UNION SELECT substr(catalog_ref, instr(catalog_ref, '@') + 1) FROM prow_runs
      UNION SELECT digest FROM prow_run_images);

-- name: PruneOrphanFBCRelatedImages :execrows
DELETE FROM fbc_related_images
WHERE digest NOT IN (SELECT digest FROM fbc_renders);
