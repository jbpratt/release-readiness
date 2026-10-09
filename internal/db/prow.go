package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/prow"
)

// UpsertProwRun stores a run by (job_name, build_id) and replaces its images.
func (d *DB) UpsertProwRun(ctx context.Context, r *prow.Run) error {
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		if err := q.UpsertProwRun(ctx, dbsqlc.UpsertProwRunParams{
			JobName:       r.JobName,
			BuildID:       r.BuildID,
			Application:   r.Application,
			State:         r.State,
			StartedAt:     formatOptionalTime(r.StartedAt),
			CompletedAt:   formatOptionalTime(r.CompletedAt),
			ProwUrl:       r.ProwURL,
			ArtifactState: r.ArtifactState,
			CatalogRef:    r.CatalogRef,
			FetchedAt:     r.FetchedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
		if err := q.DeleteProwRunImages(ctx, dbsqlc.DeleteProwRunImagesParams{JobName: r.JobName, BuildID: r.BuildID}); err != nil {
			return err
		}
		for _, img := range r.Images {
			if err := q.InsertProwRunImage(ctx, dbsqlc.InsertProwRunImageParams{
				JobName: r.JobName,
				BuildID: r.BuildID,
				Role:    img.Role,
				Digest:  img.Digest,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListFinishedProwBuildIDs(ctx context.Context, jobName string) ([]string, error) {
	return d.queries().ListFinishedProwBuildIDs(ctx, jobName)
}

// ListProwRunsByApplication returns every run of the application's jobs,
// newest first.
func (d *DB) ListProwRunsByApplication(ctx context.Context, application string) ([]prow.Run, error) {
	rows, err := d.queries().ListProwRunsByApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	return d.toProwRuns(ctx, rows)
}

func (d *DB) toProwRuns(ctx context.Context, rows []dbsqlc.ProwRun) ([]prow.Run, error) {
	runs := make([]prow.Run, len(rows))
	for i, r := range rows {
		imgs, err := d.queries().ListProwRunImages(ctx, dbsqlc.ListProwRunImagesParams{JobName: r.JobName, BuildID: r.BuildID})
		if err != nil {
			return nil, err
		}
		runs[i] = prow.Run{
			JobName:       r.JobName,
			BuildID:       r.BuildID,
			Application:   r.Application,
			State:         r.State,
			StartedAt:     parseOptionalTime(r.StartedAt),
			CompletedAt:   parseOptionalTime(r.CompletedAt),
			ProwURL:       r.ProwUrl,
			ArtifactState: r.ArtifactState,
			CatalogRef:    r.CatalogRef,
			FetchedAt:     parseTime(r.FetchedAt),
			Images:        make([]prow.Image, len(imgs)),
		}
		for j, img := range imgs {
			runs[i].Images[j] = prow.Image{Role: img.Role, Digest: img.Digest}
		}
	}
	return runs, nil
}

func (d *DB) UpsertProwSync(ctx context.Context, s prow.SyncState) error {
	return d.queries().UpsertProwSync(ctx, dbsqlc.UpsertProwSyncParams{
		JobName:            s.JobName,
		Application:        s.Application,
		IntervalSeconds:    int64(s.Interval / time.Second),
		LastSuccessfulSync: s.LastSuccessfulSync.UTC().Format(time.RFC3339),
	})
}

func (d *DB) ListProwSyncs(ctx context.Context) ([]prow.SyncState, error) {
	rows, err := d.queries().ListProwSyncs(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]prow.SyncState, len(rows))
	for i, r := range rows {
		states[i] = prow.SyncState{
			JobName:            r.JobName,
			Application:        r.Application,
			Interval:           time.Duration(r.IntervalSeconds) * time.Second,
			LastSuccessfulSync: parseTime(r.LastSuccessfulSync),
		}
	}
	return states, nil
}
