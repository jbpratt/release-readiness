package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/fbc"
)

// ListFBCCatalogCandidates returns up to limit quay-operator FBC images, one
// per FBC application's newest Snapshot that ART did not stage, whose catalog
// is unread or failed before retryBefore.
func (d *DB) ListFBCCatalogCandidates(ctx context.Context, retryBefore time.Time, limit int) ([]string, error) {
	return d.queries().ListFBCCatalogCandidates(ctx, dbsqlc.ListFBCCatalogCandidatesParams{
		CheckedAt: retryBefore.UTC().Format(time.RFC3339),
		Limit:     int64(limit),
	})
}

// ReplaceFBCCatalog stores the outcome of reading the catalog image digest.
func (d *DB) ReplaceFBCCatalog(ctx context.Context, digest, state string, bundles []fbc.Bundle, checkedAt time.Time) error {
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		if err := q.UpsertFBCCatalog(ctx, dbsqlc.UpsertFBCCatalogParams{
			Digest:    digest,
			State:     state,
			CheckedAt: checkedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
		if err := q.DeleteFBCCatalogBundles(ctx, digest); err != nil {
			return err
		}
		for _, b := range bundles {
			if err := q.InsertFBCCatalogBundle(ctx, dbsqlc.InsertFBCCatalogBundleParams{
				CatalogDigest: digest,
				Package:       b.Package,
				Channel:       b.Channel,
				BundleName:    b.Name,
				BundleRef:     b.Image,
				BundleDigest:  b.Digest,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// LatestFBCCatalog returns the newest Snapshot of application holding
// component that ART did not stage, with the quay-operator entries of channel
// in its catalog, or nil if there is none.
func (d *DB) LatestFBCCatalog(ctx context.Context, application, component, channel string) (*fbc.Catalog, error) {
	q := d.queries()
	row, err := q.GetLatestSnapshotImage(ctx, dbsqlc.GetLatestSnapshotImageParams{Application: application, Component: component})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cat := &fbc.Catalog{Snapshot: row.Name}
	_, digest, _ := strings.Cut(row.ImageUrl, "@")
	cat.State, err = q.GetFBCCatalogState(ctx, digest)
	if errors.Is(err, sql.ErrNoRows) {
		return cat, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.ListFBCCatalogBundles(ctx, dbsqlc.ListFBCCatalogBundlesParams{CatalogDigest: digest, Package: fbc.Package, Channel: channel})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		cat.Bundles = append(cat.Bundles, fbc.Bundle{Package: r.Package, Channel: r.Channel, Name: r.BundleName, Image: r.BundleRef, Digest: r.BundleDigest})
	}
	return cat, nil
}
