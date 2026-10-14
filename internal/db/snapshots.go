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

// SnapshotNames returns the names of every stored Snapshot.
func (d *DB) SnapshotNames(ctx context.Context) (map[string]bool, error) {
	names, err := d.queries().ListSnapshotNames(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

func (d *DB) CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, imageURL string) error {
	return d.queries().CreateSnapshotComponent(ctx, dbsqlc.CreateSnapshotComponentParams{
		SnapshotID: snapshotID,
		Component:  component,
		ImageUrl:   imageURL,
	})
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
			builds = append(builds, model.ReleaseSnapshot{Name: r.Name, CreatedAt: parseTime(r.CreatedAt)})
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
	components, err := d.queries().ListSnapshotComponents(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	snap := &model.ReleaseSnapshot{Name: row.Name, CreatedAt: parseTime(row.CreatedAt), Components: make([]model.SnapshotImage, len(components))}
	for i, c := range components {
		snap.Components[i] = model.SnapshotImage{Name: c.Component, Image: c.ImageUrl}
	}
	return snap, nil
}
