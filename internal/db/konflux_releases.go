package db

import (
	"context"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertKonfluxRelease(ctx context.Context, r *model.KonfluxRelease) error {
	return d.queries().UpsertKonfluxRelease(ctx, dbsqlc.UpsertKonfluxReleaseParams{
		Name:           r.Name,
		Application:    r.Application,
		Snapshot:       r.Snapshot,
		ReleasePlan:    r.ReleasePlan,
		ReleasedStatus: r.ReleasedStatus,
		ReleasedReason: r.ReleasedReason,
		FailedTask:     r.FailedTask,
		FailedStep:     r.FailedStep,
		CreatedAt:      r.CreatedAt.UTC().Format(time.RFC3339),
		StartTime:      formatOptionalTime(r.StartTime),
		CompletionTime: formatOptionalTime(r.CompletionTime),
	})
}

func (d *DB) ListKonfluxReleases(ctx context.Context, application string, limit, offset int) ([]model.KonfluxRelease, error) {
	var rows []dbsqlc.KonfluxRelease
	var err error
	if application != "" {
		rows, err = d.queries().ListKonfluxReleasesByApplication(ctx, dbsqlc.ListKonfluxReleasesByApplicationParams{
			Application: application,
			Limit:       int64(limit),
			Offset:      int64(offset),
		})
	} else {
		rows, err = d.queries().ListAllKonfluxReleases(ctx, dbsqlc.ListAllKonfluxReleasesParams{
			Limit:  int64(limit),
			Offset: int64(offset),
		})
	}
	if err != nil {
		return nil, err
	}
	releases := make([]model.KonfluxRelease, len(rows))
	for i, r := range rows {
		releases[i] = toKonfluxRelease(r)
	}
	return releases, nil
}

func toKonfluxRelease(r dbsqlc.KonfluxRelease) model.KonfluxRelease {
	return model.KonfluxRelease{
		Name:           r.Name,
		Application:    r.Application,
		Snapshot:       r.Snapshot,
		ReleasePlan:    r.ReleasePlan,
		ReleasedStatus: r.ReleasedStatus,
		ReleasedReason: r.ReleasedReason,
		FailedTask:     r.FailedTask,
		FailedStep:     r.FailedStep,
		CreatedAt:      parseTime(r.CreatedAt),
		StartTime:      parseOptionalTime(r.StartTime),
		CompletionTime: parseOptionalTime(r.CompletionTime),
	}
}
