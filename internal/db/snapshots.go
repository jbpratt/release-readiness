package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/releaseview"
)

func (d *DB) CreateSnapshot(ctx context.Context, application, name string, createdAt time.Time) (id int64, err error) {
	return d.queries().CreateSnapshot(ctx, dbsqlc.CreateSnapshotParams{
		Application: application,
		Name:        name,
		CreatedAt:   createdAt.UTC().Format(time.RFC3339),
	})
}

func (d *DB) SnapshotExistsByName(ctx context.Context, name string) (bool, error) {
	count, err := d.queries().SnapshotExistsByName(ctx, name)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, imageURL string) error {
	return d.queries().CreateSnapshotComponent(ctx, dbsqlc.CreateSnapshotComponentParams{
		SnapshotID: snapshotID,
		Component:  component,
		ImageUrl:   imageURL,
	})
}

func (d *DB) listSnapshotComponents(ctx context.Context, snapshotID int64) ([]model.ComponentRecord, error) {
	rows, err := d.queries().ListSnapshotComponents(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	components := make([]model.ComponentRecord, len(rows))
	for i, r := range rows {
		components[i] = model.ComponentRecord{Component: r.Component, ImageURL: r.ImageUrl}
	}
	return components, nil
}

// ListComponentCandidates returns every component row of the given
// applications' snapshots, for releaseview.Select to narrow down.
func (d *DB) ListComponentCandidates(ctx context.Context, applications []string) ([]releaseview.Component, error) {
	rows, err := d.queries().ListComponentCandidates(ctx, applications)
	if err != nil {
		return nil, err
	}
	components := make([]releaseview.Component, len(rows))
	for i, r := range rows {
		components[i] = releaseview.Component{
			Name:        r.Component,
			Image:       r.ImageUrl,
			Application: r.Application,
			CreatedAt:   parseTime(r.CreatedAt),
			SnapshotID:  r.SnapshotID,
			RowID:       r.ID,
		}
	}
	return components, nil
}

// NewestStreamBuild returns the newest Snapshot of application that ART
// built from its stream, as GetReleaseSnapshot does, or nil when there is none.
func (d *DB) NewestStreamBuild(ctx context.Context, application string) (*model.ReleaseSnapshot, error) {
	name, err := d.queries().NewestStreamSnapshot(ctx, application)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetReleaseSnapshot(ctx, name)
}

// NewestStreamBuildWith returns the newest Snapshot of application that ART
// built from its stream whose images hold every digest, as NewestStreamBuild
// does, or nil when there is none.
func (d *DB) NewestStreamBuildWith(ctx context.Context, application string, digests []string) (*model.ReleaseSnapshot, error) {
	var names []string
	for i, digest := range digests {
		holding, err := d.queries().ListStreamSnapshotsWithDigest(ctx, dbsqlc.ListStreamSnapshotsWithDigestParams{Application: application, Digest: digest})
		if err != nil {
			return nil, err
		}
		if i == 0 {
			names = holding
		}
		names = slices.DeleteFunc(names, func(n string) bool { return !slices.Contains(holding, n) })
	}
	if len(names) == 0 {
		return nil, nil
	}
	return d.GetReleaseSnapshot(ctx, names[0])
}

// GetReleaseSnapshot returns a stored Snapshot with its component images.
func (d *DB) GetReleaseSnapshot(ctx context.Context, name string) (*model.ReleaseSnapshot, error) {
	row, err := d.queries().GetSnapshotRow(ctx, name)
	if err != nil {
		return nil, err
	}
	components, err := d.listSnapshotComponents(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	snap := model.ReleaseSnapshot{
		Application: row.Application,
		Name:        row.Name,
		CreatedAt:   parseTime(row.CreatedAt),
		Components:  make([]model.SnapshotImage, len(components)),
	}
	for i, c := range components {
		snap.Components[i] = model.SnapshotImage{Name: c.Component, Image: c.ImageURL}
	}
	return &snap, nil
}
