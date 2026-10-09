package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/releaseview"
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
			Component: r.Component,
			GitSHA:    r.GitSha,
			ImageURL:  r.ImageUrl,
			GitURL:    r.GitUrl,
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

// ListReleaseSnapshots returns a newest-first page of the Snapshots of apps
// that belong to the release konfluxApp, each with the Konflux Releases that
// name it. Snapshots a Release names but that are no longer stored come back
// as Missing, dated by their oldest Release.
func (d *DB) ListReleaseSnapshots(ctx context.Context, konfluxApp string, apps []string, withRelease bool, limit, offset int) ([]model.ReleaseSnapshot, error) {
	var missingApps []string
	for _, app := range apps {
		// A missing base Snapshot has no components to tie it to a version.
		if app != releaseview.BaseImagesApp {
			missingApps = append(missingApps, app)
		}
	}
	rows, err := d.queries().ListReleaseSnapshots(ctx, dbsqlc.ListReleaseSnapshotsParams{
		Applications:        apps,
		Application:         releaseview.BaseImagesApp,
		Component:           konfluxApp + "-%",
		Column4:             withRelease, // sqlc cannot name a bare boolean parameter
		MissingApplications: missingApps,
		Limit:               int64(limit),
		Offset:              int64(offset),
	})
	if err != nil {
		return nil, err
	}
	snapshots := make([]model.ReleaseSnapshot, len(rows))
	for i, r := range rows {
		snapshots[i] = model.ReleaseSnapshot{
			Application:    r.Application,
			Name:           r.Name,
			CreatedAt:      parseTime(r.CreatedAt),
			ComponentCount: int(r.ComponentCount),
			Missing:        r.Missing == 1,
		}
	}
	if err := d.attachKonfluxReleases(ctx, snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
}

// GetReleaseSnapshot returns a stored Snapshot with its component images and
// the Konflux Releases that name it.
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
		Application:    row.Application,
		Name:           row.Name,
		CreatedAt:      parseTime(row.CreatedAt),
		ComponentCount: len(components),
		Components:     make([]model.SnapshotImage, len(components)),
	}
	for i, c := range components {
		snap.Components[i] = model.SnapshotImage{Name: c.Component, Image: c.ImageURL}
	}
	snaps := []model.ReleaseSnapshot{snap}
	if err := d.attachKonfluxReleases(ctx, snaps); err != nil {
		return nil, err
	}
	return &snaps[0], nil
}

// attachKonfluxReleases sets each snapshot's Releases: those naming it from
// the same application.
func (d *DB) attachKonfluxReleases(ctx context.Context, snapshots []model.ReleaseSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	names := make([]string, len(snapshots))
	for i, s := range snapshots {
		names[i] = s.Name
	}
	rows, err := d.queries().ListKonfluxReleasesBySnapshots(ctx, names)
	if err != nil {
		return err
	}
	byKey := map[[2]string][]model.KonfluxRelease{}
	for _, r := range rows {
		k := [2]string{r.Application, r.Snapshot}
		byKey[k] = append(byKey[k], toKonfluxRelease(r))
	}
	for i := range snapshots {
		snapshots[i].Releases = byKey[[2]string{snapshots[i].Application, snapshots[i].Name}]
	}
	return nil
}

func toSnapshotRecord(r dbsqlc.Snapshot) model.SnapshotRecord {
	return model.SnapshotRecord{
		ID:          r.ID,
		Application: r.Application,
		Name:        r.Name,
		CreatedAt:   parseTime(r.CreatedAt),
	}
}
