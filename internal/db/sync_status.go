package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
)

func (d *DB) UpsertSyncStatus(ctx context.Context, source string, lastSuccess time.Time) error {
	return d.queries().UpsertSyncStatus(ctx, dbsqlc.UpsertSyncStatusParams{Source: source, LastSuccess: lastSuccess.UTC()})
}

// SyncStatus returns each source's last successful pass, by source.
func (d *DB) SyncStatus(ctx context.Context) (map[string]time.Time, error) {
	rows, err := d.queries().ListSyncStatus(ctx)
	if err != nil {
		return nil, err
	}
	last := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		last[r.Source] = r.LastSuccess.UTC()
	}
	return last, nil
}
