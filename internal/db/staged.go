package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertStagedSnapshot(ctx context.Context, name, assembly, kind, env string, createdAt time.Time) error {
	return d.queries().UpsertStagedSnapshot(ctx, dbsqlc.UpsertStagedSnapshotParams{
		Name:      name,
		Assembly:  assembly,
		Kind:      kind,
		Env:       env,
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
	})
}

// LatestStagedSnapshot returns the newest stage Snapshot of kind for assembly
// with the newest Release naming it, or nil when there is none.
func (d *DB) LatestStagedSnapshot(ctx context.Context, assembly, kind string) (*model.StagedSnapshot, error) {
	row, err := d.queries().LatestStagedSnapshot(ctx, dbsqlc.LatestStagedSnapshotParams{Assembly: assembly, Kind: kind})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := &model.StagedSnapshot{Name: row.Name, Assembly: row.Assembly, Kind: row.Kind, CreatedAt: parseTime(row.CreatedAt)}
	releases, err := d.queries().ListKonfluxReleasesBySnapshots(ctx, []string{row.Name})
	if err != nil {
		return nil, err
	}
	if len(releases) > 0 {
		r := toKonfluxRelease(releases[0])
		s.Release = &r
	}
	return s, nil
}
