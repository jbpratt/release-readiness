package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/reconcile"
)

var _ reconcile.ProvenanceStore = (*DB)(nil)

// UpsertSource stores s's name and source repo for its stored ART record.
func (d *DB) UpsertSource(ctx context.Context, s reconcile.Source) error {
	return d.UpsertArtSourceRepo(ctx, s.RecordID, s.Name, s.SourceRepo)
}

func (d *DB) SourceByRecord(ctx context.Context, recordID string) (reconcile.Source, bool, error) {
	r, err := d.queries().GetSourceByRecord(ctx, recordID)
	if errors.Is(err, sql.ErrNoRows) {
		return reconcile.Source{}, false, nil
	}
	if err != nil {
		return reconcile.Source{}, false, err
	}
	return reconcile.Source{RecordID: r.RecordID, Name: r.Name, NVR: r.Nvr, Digest: r.Digest, SourceRepo: r.SourceRepo, Commitish: r.Commitish}, true, nil
}

func (d *DB) SourcesByDigest(ctx context.Context, digest string) ([]reconcile.Source, error) {
	rows, err := d.queries().ListSourcesByDigest(ctx, digest)
	if err != nil {
		return nil, err
	}
	sources := make([]reconcile.Source, len(rows))
	for i, r := range rows {
		sources[i] = reconcile.Source{RecordID: r.RecordID, Name: r.Name, NVR: r.Nvr, Digest: r.Digest, SourceRepo: r.SourceRepo, Commitish: r.Commitish}
	}
	return sources, nil
}

func (d *DB) GetBaseline(ctx context.Context, stream, operator, ocp string) (reconcile.Baseline, bool, error) {
	r, err := d.queries().GetReconcileBaseline(ctx, dbsqlc.GetReconcileBaselineParams{Stream: stream, Operator: operator, Ocp: ocp})
	if errors.Is(err, sql.ErrNoRows) {
		return reconcile.Baseline{}, false, nil
	}
	if err != nil {
		return reconcile.Baseline{}, false, err
	}
	return reconcile.Baseline{Stream: r.Stream, Operator: r.Operator, OCP: r.Ocp, FBCDigest: r.FbcDigest, FBCNVR: r.FbcNvr}, true, nil
}

func (d *DB) InsertBaseline(ctx context.Context, b reconcile.Baseline) error {
	return d.queries().InsertReconcileBaseline(ctx, dbsqlc.InsertReconcileBaselineParams{
		Stream:    b.Stream,
		Operator:  b.Operator,
		Ocp:       b.OCP,
		FbcDigest: b.FBCDigest,
		FbcNvr:    b.FBCNVR,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}
