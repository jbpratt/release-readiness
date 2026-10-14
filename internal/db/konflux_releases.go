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
		CompletionTime: formatOptionalTime(r.CompletionTime),
	})
}

// KonfluxReleases returns every stored Release by name.
func (d *DB) KonfluxReleases(ctx context.Context) (map[string]model.KonfluxRelease, error) {
	rows, err := d.queries().ListKonfluxReleases(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.KonfluxRelease, len(rows))
	for _, r := range rows {
		m[r.Name] = toKonfluxRelease(r)
	}
	return m, nil
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
		CompletionTime: parseOptionalTime(r.CompletionTime),
	}
}
