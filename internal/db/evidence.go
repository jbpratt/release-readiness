package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/github"
)

// StoreEvidenceScan upserts s and replaces its ticket evidence.
func (d *DB) StoreEvidenceScan(ctx context.Context, s github.Scan) error {
	checked := s.CheckedAt.UTC().Format(time.RFC3339)
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		if err := q.UpsertEvidenceScan(ctx, dbsqlc.UpsertEvidenceScanParams{
			SnapshotName: s.Snapshot,
			Component:    s.Component,
			ImageDigest:  s.ImageDigest,
			UpstreamRepo: s.Repo,
			BaseSha:      s.BaseSHA,
			HeadSha:      s.HeadSHA,
			State:        s.State,
			Reason:       s.Reason,
			CheckedAt:    checked,
		}); err != nil {
			return err
		}
		if err := q.DeleteScanEvidence(ctx, dbsqlc.DeleteScanEvidenceParams{SnapshotName: s.Snapshot, Component: s.Component, ImageDigest: s.ImageDigest}); err != nil {
			return err
		}
		for _, e := range s.Evidence {
			if err := q.InsertTicketEvidence(ctx, dbsqlc.InsertTicketEvidenceParams{
				SnapshotName: s.Snapshot,
				Component:    s.Component,
				ImageDigest:  s.ImageDigest,
				TicketKey:    e.Key,
				CommitSha:    e.CommitSHA,
				CommitUrl:    e.CommitURL,
				PrUrl:        e.PRURL,
				Source:       e.Source,
				CheckedAt:    checked,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListEvidenceScans returns snapshot's scans, one per component image, each
// with its ticket evidence.
func (d *DB) ListEvidenceScans(ctx context.Context, snapshot string) ([]github.Scan, error) {
	rows, err := d.queries().ListEvidenceScans(ctx, snapshot)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	evidence, err := d.scanEvidence(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	scans := make([]github.Scan, len(rows))
	for i, r := range rows {
		scans[i] = toScan(r)
		scans[i].Evidence = evidence[[2]string{r.Component, r.ImageDigest}]
	}
	return scans, nil
}

// CompleteScan returns the newest complete scan of repo's base...head, from
// any Snapshot, with its ticket evidence, or nil.
func (d *DB) CompleteScan(ctx context.Context, repo, base, head string) (*github.Scan, error) {
	r, err := d.queries().GetCompleteScanByRange(ctx, dbsqlc.GetCompleteScanByRangeParams{UpstreamRepo: repo, BaseSha: base, HeadSha: head})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	evidence, err := d.scanEvidence(ctx, r.SnapshotName)
	if err != nil {
		return nil, err
	}
	s := toScan(r)
	s.Evidence = evidence[[2]string{r.Component, r.ImageDigest}]
	return &s, nil
}

// scanEvidence returns snapshot's ticket evidence by component and digest.
func (d *DB) scanEvidence(ctx context.Context, snapshot string) (map[[2]string][]github.Evidence, error) {
	rows, err := d.queries().ListSnapshotTicketEvidence(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	evidence := make(map[[2]string][]github.Evidence)
	for _, r := range rows {
		k := [2]string{r.Component, r.ImageDigest}
		evidence[k] = append(evidence[k], github.Evidence{Key: r.TicketKey, CommitSHA: r.CommitSha, CommitURL: r.CommitUrl, PRURL: r.PrUrl, Source: r.Source})
	}
	return evidence, nil
}

func toScan(r dbsqlc.BuildEvidenceScan) github.Scan {
	return github.Scan{
		Snapshot:    r.SnapshotName,
		Component:   r.Component,
		ImageDigest: r.ImageDigest,
		Repo:        r.UpstreamRepo,
		BaseSHA:     r.BaseSha,
		HeadSHA:     r.HeadSha,
		State:       r.State,
		Reason:      r.Reason,
		CheckedAt:   parseTime(r.CheckedAt),
	}
}
