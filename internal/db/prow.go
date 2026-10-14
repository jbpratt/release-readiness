package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/prow"
)

// UpsertProwRun stores a run by (job_name, build_id) and replaces its images
// and whether its install failed.
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
		if err := q.DeleteProwInstallFailure(ctx, dbsqlc.DeleteProwInstallFailureParams{JobName: r.JobName, BuildID: r.BuildID}); err != nil {
			return err
		}
		if !r.InstallFailed {
			return nil
		}
		return q.InsertProwInstallFailure(ctx, dbsqlc.InsertProwInstallFailureParams{JobName: r.JobName, BuildID: r.BuildID})
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
	imgRows, err := d.queries().ListProwRunImagesByApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	imgs := map[[2]string][]prow.Image{}
	for _, img := range imgRows {
		k := [2]string{img.JobName, img.BuildID}
		imgs[k] = append(imgs[k], prow.Image{Role: img.Role, Digest: img.Digest})
	}
	runs := make([]prow.Run, len(rows))
	for i, r := range rows {
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
			InstallFailed: r.InstallFailed,
			FetchedAt:     parseTime(r.FetchedAt),
			Images:        imgs[[2]string{r.JobName, r.BuildID}],
		}
	}
	return runs, nil
}

// ListProwRunStarts returns the build ID and start of each of the job's runs,
// newest first.
func (d *DB) ListProwRunStarts(ctx context.Context, jobName string) ([]prow.Run, error) {
	rows, err := d.queries().ListProwRunStarts(ctx, jobName)
	if err != nil {
		return nil, err
	}
	runs := make([]prow.Run, len(rows))
	for i, r := range rows {
		runs[i] = prow.Run{JobName: jobName, BuildID: r.BuildID, StartedAt: parseOptionalTime(r.StartedAt)}
	}
	return runs, nil
}
