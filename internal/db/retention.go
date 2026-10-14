package db

import (
	"context"
	"fmt"
	"time"
)

// Prune deletes what is older than cutoff and not kept by the rules in
// queries/retention.sql, and returns the rows deleted by table. Each
// statement commits on its own, so no write lock is held for the whole prune.
func (d *DB) Prune(ctx context.Context, cutoff time.Time) (map[string]int64, error) {
	c := cutoff.UTC().Format(time.RFC3339)
	q := d.queries()
	// Snapshots go before their orphaned staged rows, and every digest owner
	// before the digest caches.
	steps := []struct {
		table string
		fn    func() (int64, error)
	}{
		{"konflux_releases", func() (int64, error) { return q.PruneFailedReleases(ctx, c) }},
		{"snapshots", func() (int64, error) { return q.PruneSnapshots(ctx, c) }},
		{"staged_snapshots", func() (int64, error) { return q.PruneOrphanStagedSnapshots(ctx) }},
		{"prow_runs", func() (int64, error) { return q.PruneProwRuns(ctx, c) }},
		{"art_builds", func() (int64, error) { return q.PruneArtBuilds(ctx, c) }},
		{"image_scans", func() (int64, error) { return q.PruneImageScans(ctx, c) }},
		{"fbc_renders", func() (int64, error) { return q.PruneFBCRenders(ctx, c) }},
		{"fbc_related_images", func() (int64, error) { return q.PruneOrphanFBCRelatedImages(ctx) }},
	}
	deleted := make(map[string]int64, len(steps))
	for _, s := range steps {
		n, err := s.fn()
		if err != nil {
			return deleted, fmt.Errorf("prune %s: %w", s.table, err)
		}
		deleted[s.table] = n
	}
	return deleted, nil
}
