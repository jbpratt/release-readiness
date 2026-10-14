package shipment

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

// Store is the subset of the database layer the syncer needs.
type Store interface {
	UpsertShipmentMR(ctx context.Context, mr *model.ShipmentMR) error
}

// Syncer stores the shipment MR of each version that versions returns, a
// quay-vX.Y.Z whose assembly is X.Y.Z.
type Syncer struct {
	client   *Client
	store    Store
	versions func(context.Context) ([]string, error)
	logger   *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewSyncer(client *Client, store Store, versions func(context.Context) ([]string, error), logger *slog.Logger) *Syncer {
	return &Syncer{client: client, store: store, versions: versions, logger: logger}
}

// Run syncs immediately and then every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	s.SyncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

func (s *Syncer) SyncOnce(ctx context.Context) {
	versions, err := s.versions(ctx)
	if err != nil {
		s.logger.Error("list versions", "error", err)
		s.Status.Report(err)
		return
	}
	var pass syncstatus.Pass
	for _, v := range versions {
		if err := s.syncVersion(ctx, v); err != nil {
			s.logger.Error("sync version", "version", v, "error", err)
			pass.Add(fmt.Errorf("%s: %w", v, err))
		}
	}
	s.Status.Report(pass.Err())
}

func (s *Syncer) syncVersion(ctx context.Context, version string) error {
	assembly := strings.TrimPrefix(version, "quay-v")
	mr, source, err := s.find(ctx, assembly)
	if err != nil || mr == nil {
		return err
	}
	rec := &model.ShipmentMR{
		Version:             version,
		Assembly:            assembly,
		IID:                 mr.IID,
		WebURL:              mr.WebURL,
		State:               mr.State,
		Draft:               mr.Draft,
		Labels:              mr.Labels,
		DetailedMergeStatus: mr.DetailedMergeStatus,
		MergedAt:            mr.MergedAt,
		UpdatedAt:           mr.UpdatedAt,
		Source:              source,
		CheckedAt:           time.Now().UTC(),
	}
	if mr.HeadPipeline != nil {
		rec.HeadPipelineStatus = mr.HeadPipeline.Status
	}
	return s.store.UpsertShipmentMR(ctx, rec)
}

// find returns the MR the assembly's pointer names, unless it is closed, and
// otherwise the one search prefers, else the closed pointer MR, with where it
// came from.
func (s *Syncer) find(ctx context.Context, assembly string) (*MR, string, error) {
	iid, err := s.client.pointer(ctx, assembly)
	if err != nil {
		return nil, "", err
	}
	var closed *MR
	if iid != 0 {
		mr, err := s.client.mr(ctx, iid)
		if err != nil {
			return nil, "", err
		}
		if mr != nil && mr.State != "closed" {
			return mr, "pointer", nil
		}
		closed = mr
	}
	mr, err := s.search(ctx, assembly)
	if err == nil && mr == nil && closed != nil {
		return closed, "pointer", nil
	}
	return mr, "search", err
}

// search returns an open non-draft shipment MR of the assembly, else the
// most recently updated merged one, else closed one, or nil. Titles are not trusted: an
// MR counts only when it is on one of the assembly's prepare-shipment
// branches and changes Quay shipment files, which also keeps out another
// product's assembly of the same name. Changes are read only for the MRs
// the order reaches.
func (s *Syncer) search(ctx context.Context, assembly string) (*MR, error) {
	mrs, err := s.client.search(ctx, assembly)
	if err != nil {
		return nil, err
	}
	branch := "prepare-shipment-" + assembly + "-"
	for _, want := range []func(MR) bool{
		func(m MR) bool { return m.State == "opened" && !m.Draft },
		func(m MR) bool { return m.State == "merged" },
		func(m MR) bool { return m.State == "closed" },
	} {
		for _, m := range mrs {
			if !want(m) || !strings.HasPrefix(m.SourceBranch, branch) {
				continue
			}
			full, err := s.client.changes(ctx, m.IID)
			if err != nil {
				return nil, err
			}
			if full != nil && slices.ContainsFunc(full.Changes, quayFile) {
				return full, nil
			}
		}
	}
	return nil, nil
}

func quayFile(c Change) bool {
	return strings.HasPrefix(c.NewPath, "shipment/quay/") || strings.HasPrefix(c.OldPath, "shipment/quay/")
}

// Derive sets the MR's stage and flags from its state, labels and statuses.
func Derive(mr *model.ShipmentMR) {
	switch {
	case mr.State == "merged":
		mr.Stage = "shipped"
	case mr.State == "closed":
		mr.Stage = "closed"
	case slices.Contains(mr.Labels, "prod-release-success"):
		mr.Stage = "prod_released"
	case slices.Contains(mr.Labels, "stage-release-success"):
		mr.Stage = "stage_released"
	default:
		mr.Stage = "open"
	}
	open := mr.State == "opened"
	mr.PipelineFailed = open && mr.HeadPipelineStatus == "failed"
	mr.NeedsApproval = open && mr.DetailedMergeStatus == "not_approved"
}
