package konflux

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/quay/release-readiness/internal/kube"
	"github.com/quay/release-readiness/internal/model"
)

// Store is the subset of the database layer needed by the Konflux syncer.
type Store interface {
	SnapshotExistsByName(ctx context.Context, name string) (bool, error)
	CreateSnapshotWithComponents(ctx context.Context, snap model.Snapshot, testsPassed bool, createdAt time.Time) (*model.SnapshotRecord, error)
}

// TxFunc wraps a function in a database transaction, passing a tx-scoped Store.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Syncer orchestrates periodic Konflux Snapshot synchronisation into a Store.
type Syncer struct {
	client    *kube.Client
	namespace string
	store     Store
	withTx    TxFunc
	logger    *slog.Logger
}

// NewSyncer creates a Syncer that lists Snapshots in namespace via client and persists them to store.
func NewSyncer(client *kube.Client, namespace string, store Store, withTx TxFunc, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, namespace: namespace, store: store, withTx: withTx, logger: logger}
}

// Run performs an immediate sync and then repeats every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	s.syncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

// syncOnce lists all Snapshots and ingests the new ones that have a test result.
func (s *Syncer) syncOnce(ctx context.Context) {
	snapshots, err := s.listSnapshots(ctx)
	if err != nil {
		s.logger.Error("list snapshots", "error", err)
		return
	}

	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].Metadata.CreationTimestamp.Before(snapshots[j].Metadata.CreationTimestamp)
	})

	var inserted, skippedExisting, skippedNoResult int
	for _, raw := range snapshots {
		if ctx.Err() != nil {
			return
		}

		exists, err := s.store.SnapshotExistsByName(ctx, raw.Metadata.Name)
		if err != nil {
			s.logger.Error("check snapshot", "snapshot", raw.Metadata.Name, "error", err)
			continue
		}
		if exists {
			skippedExisting++
			continue
		}

		testsPassed, ok := raw.testResult()
		if !ok {
			skippedNoResult++
			continue
		}

		snap := Convert(raw.Spec, raw.Metadata.Name)
		if err := s.withTx(ctx, func(txStore Store) error {
			_, err := txStore.CreateSnapshotWithComponents(ctx, snap, testsPassed, raw.Metadata.CreationTimestamp)
			return err
		}); err != nil {
			s.logger.Error("ingest snapshot", "snapshot", raw.Metadata.Name, "error", err)
			continue
		}
		inserted++
	}

	s.logger.Info("sync complete",
		"listed", len(snapshots),
		"inserted", inserted,
		"skipped_existing", skippedExisting,
		"skipped_no_result", skippedNoResult,
	)
}

func (s *Syncer) listSnapshots(ctx context.Context) ([]rawSnapshot, error) {
	var all []rawSnapshot
	err := s.client.List(ctx, snapshotsPath(s.namespace), func(items json.RawMessage) error {
		var page []rawSnapshot
		if err := json.Unmarshal(items, &page); err != nil {
			return err
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}
