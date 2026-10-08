package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) CreateSnapshot(ctx context.Context, application, name string, createdAt time.Time) (*model.SnapshotRecord, error) {
	id, err := d.queries().CreateSnapshot(ctx, dbsqlc.CreateSnapshotParams{
		Application: application,
		Name:        name,
		CreatedAt:   createdAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return &model.SnapshotRecord{
		ID:          id,
		Application: application,
		Name:        name,
		CreatedAt:   createdAt.UTC(),
	}, nil
}

func (d *DB) SnapshotExistsByName(ctx context.Context, name string) (bool, error) {
	count, err := d.queries().SnapshotExistsByName(ctx, name)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) GetSnapshotByID(ctx context.Context, id int64) (*model.SnapshotRecord, error) {
	row, err := d.queries().GetSnapshotByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s := toSnapshotRecord(row)
	return &s, nil
}

func (d *DB) GetSnapshotByName(ctx context.Context, name string) (*model.SnapshotRecord, error) {
	row, err := d.queries().GetSnapshotRow(ctx, name)
	if err != nil {
		return nil, err
	}
	s := toSnapshotRecord(row)

	components, err := d.listSnapshotComponents(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	s.Components = components

	return &s, nil
}

func (d *DB) CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, gitSHA, imageURL, gitURL string) error {
	return d.queries().CreateSnapshotComponent(ctx, dbsqlc.CreateSnapshotComponentParams{
		SnapshotID: snapshotID,
		Component:  component,
		GitSha:     gitSHA,
		ImageUrl:   imageURL,
		GitUrl:     gitURL,
	})
}

func (d *DB) listSnapshotComponents(ctx context.Context, snapshotID int64) ([]model.ComponentRecord, error) {
	rows, err := d.queries().ListSnapshotComponents(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	components := make([]model.ComponentRecord, len(rows))
	for i, r := range rows {
		components[i] = model.ComponentRecord{
			ID:         r.ID,
			SnapshotID: r.SnapshotID,
			Component:  r.Component,
			GitSHA:     r.GitSha,
			ImageURL:   r.ImageUrl,
			GitURL:     r.GitUrl,
		}
	}
	return components, nil
}

func (d *DB) ListSnapshots(ctx context.Context, application string, limit, offset int) ([]model.SnapshotRecord, error) {
	var rows []dbsqlc.Snapshot
	var err error
	if application != "" {
		rows, err = d.queries().ListSnapshotsByApplication(ctx, dbsqlc.ListSnapshotsByApplicationParams{
			Application: application,
			Limit:       int64(limit),
			Offset:      int64(offset),
		})
	} else {
		rows, err = d.queries().ListAllSnapshots(ctx, dbsqlc.ListAllSnapshotsParams{
			Limit:  int64(limit),
			Offset: int64(offset),
		})
	}
	if err != nil {
		return nil, err
	}
	snapshots := make([]model.SnapshotRecord, len(rows))
	for i, r := range rows {
		snapshots[i] = toSnapshotRecord(r)
	}
	return snapshots, nil
}

func (d *DB) LatestSnapshotPerApplication(ctx context.Context) ([]model.ApplicationSummary, error) {
	rows, err := d.queries().LatestSnapshotPerApplication(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]model.ApplicationSummary, len(rows))
	for i, r := range rows {
		s := model.SnapshotRecord{
			ID:          r.ID,
			Application: r.Application,
			Name:        r.Name,
			CreatedAt:   parseTime(r.CreatedAt),
		}
		summaries[i] = model.ApplicationSummary{
			Application:    r.Application,
			LatestSnapshot: &s,
			SnapshotCount:  int(r.Cnt),
		}
	}
	return summaries, nil
}

func toSnapshotRecord(r dbsqlc.Snapshot) model.SnapshotRecord {
	return model.SnapshotRecord{
		ID:          r.ID,
		Application: r.Application,
		Name:        r.Name,
		CreatedAt:   parseTime(r.CreatedAt),
	}
}
