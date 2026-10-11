package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
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

// StreamBuilds returns the Snapshots of application that ART built from its
// stream, newest first, with their component images, and the NVR of each
// image whose ART build history record is resolved, by image.
func (d *DB) StreamBuilds(ctx context.Context, application string) ([]model.ReleaseSnapshot, map[string]string, error) {
	rows, err := d.queries().ListStreamBuildImages(ctx, application)
	if err != nil {
		return nil, nil, err
	}
	var builds []model.ReleaseSnapshot
	nvrs := map[string]string{}
	for _, r := range rows {
		if n := len(builds); n == 0 || builds[n-1].Name != r.Name {
			builds = append(builds, model.ReleaseSnapshot{Application: application, Name: r.Name, CreatedAt: parseTime(r.CreatedAt)})
		}
		b := &builds[len(builds)-1]
		b.Components = append(b.Components, model.SnapshotImage{Name: r.Component, Image: r.ImageUrl})
		if r.Nvr != "" {
			nvrs[r.ImageUrl] = r.Nvr
		}
	}
	return builds, nvrs, nil
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
