package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/scan"
)

func (d *DB) UpsertImageScan(ctx context.Context, s scan.Scan) error {
	p := dbsqlc.UpsertImageScanParams{
		Digest:      s.Digest,
		State:       s.State,
		PipelineRun: s.PipelineRun,
		DetailUrl:   s.DetailURL,
		CheckedAt:   s.CheckedAt.UTC().Format(time.RFC3339),
	}
	// Neither a struct of ints nor a string map can fail to marshal.
	if s.Counts != nil {
		b, _ := json.Marshal(s.Counts)
		p.CountsJson = sql.NullString{String: string(b), Valid: true}
	}
	if s.Reports != nil {
		b, _ := json.Marshal(s.Reports)
		p.ReportsJson = sql.NullString{String: string(b), Valid: true}
	}
	return d.queries().UpsertImageScan(ctx, p)
}

// ImageScans returns the stored scans among digests, by digest, without
// their Reports.
func (d *DB) ImageScans(ctx context.Context, digests []string) (map[string]scan.Scan, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	rows, err := d.queries().ListImageScans(ctx, digests)
	if err != nil {
		return nil, err
	}
	scans := make(map[string]scan.Scan, len(rows))
	for _, r := range rows {
		s := scan.Scan{
			Digest:      r.Digest,
			State:       r.State,
			PipelineRun: r.PipelineRun,
			DetailURL:   r.DetailUrl,
			CheckedAt:   parseTime(r.CheckedAt),
		}
		if r.CountsJson.Valid {
			if err := json.Unmarshal([]byte(r.CountsJson.String), &s.Counts); err != nil {
				return nil, err
			}
		}
		scans[r.Digest] = s
	}
	return scans, nil
}
