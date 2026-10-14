package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/shipment"
)

// UpsertShipmentMR stores the version's shipment MR.
func (d *DB) UpsertShipmentMR(ctx context.Context, mr *model.ShipmentMR) error {
	labels, err := json.Marshal(mr.Labels)
	if err != nil {
		return err
	}
	return d.queries().UpsertShipmentMR(ctx, dbsqlc.UpsertShipmentMRParams{
		Version:             mr.Version,
		Assembly:            mr.Assembly,
		Iid:                 int64(mr.IID),
		WebUrl:              mr.WebURL,
		State:               mr.State,
		Draft:               boolToInt64(mr.Draft),
		Labels:              string(labels),
		HeadPipelineStatus:  mr.HeadPipelineStatus,
		DetailedMergeStatus: mr.DetailedMergeStatus,
		MergedAt:            formatOptionalTime(mr.MergedAt),
		UpdatedAt:           mr.UpdatedAt.UTC().Format(time.RFC3339),
		Source:              mr.Source,
		CheckedAt:           mr.CheckedAt.UTC().Format(time.RFC3339),
	})
}

// ShipmentMR returns the version's shipment MR, or nil when none is stored.
func (d *DB) ShipmentMR(ctx context.Context, version string) (*model.ShipmentMR, error) {
	row, err := d.queries().GetShipmentMR(ctx, version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toShipmentMR(row), nil
}

// ShipmentMRs returns every stored shipment MR by version.
func (d *DB) ShipmentMRs(ctx context.Context) (map[string]*model.ShipmentMR, error) {
	rows, err := d.queries().ListShipmentMRs(ctx)
	if err != nil {
		return nil, err
	}
	mrs := make(map[string]*model.ShipmentMR, len(rows))
	for _, r := range rows {
		mrs[r.Version] = toShipmentMR(r)
	}
	return mrs, nil
}

func toShipmentMR(r dbsqlc.ShipmentMr) *model.ShipmentMR {
	mr := &model.ShipmentMR{
		Version:             r.Version,
		Assembly:            r.Assembly,
		IID:                 int(r.Iid),
		WebURL:              r.WebUrl,
		State:               r.State,
		Draft:               r.Draft == 1,
		HeadPipelineStatus:  r.HeadPipelineStatus,
		DetailedMergeStatus: r.DetailedMergeStatus,
		MergedAt:            parseOptionalTime(r.MergedAt),
		UpdatedAt:           parseTime(r.UpdatedAt),
		Source:              r.Source,
		CheckedAt:           parseTime(r.CheckedAt),
	}
	_ = json.Unmarshal([]byte(r.Labels), &mr.Labels)
	shipment.Derive(mr)
	return mr
}
