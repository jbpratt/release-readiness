package kube

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/quay/release-readiness/internal/konflux"
	"github.com/quay/release-readiness/internal/model"
)

var snapshotGVR = schema.GroupVersionResource{Group: "appstudio.redhat.com", Version: "v1alpha1", Resource: "snapshots"}

// NewClient builds a dynamic client from kubeconfig, or from the in-cluster
// service account when kubeconfig is empty.
func NewClient(kubeconfig string) (dynamic.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		rules := &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(kubeconfig)}
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	}
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

// Store is the subset of the database layer needed by the Konflux syncer.
type Store interface {
	SnapshotExistsByName(ctx context.Context, name string) (bool, error)
	CreateSnapshot(ctx context.Context, application, name string, createdAt time.Time) (*model.SnapshotRecord, error)
	EnsureComponent(ctx context.Context, name string) (*model.Component, error)
	CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, gitSHA, imageURL, gitURL string) error
}

// TxFunc wraps a function in a database transaction, passing a tx-scoped Store.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Syncer periodically ingests Konflux Snapshots from a namespace into a Store.
type Syncer struct {
	client    dynamic.Interface
	namespace string
	store     Store
	withTx    TxFunc
	logger    *slog.Logger
}

// NewSyncer creates a Syncer that lists Snapshots in namespace and persists them to store.
func NewSyncer(client dynamic.Interface, namespace string, store Store, withTx TxFunc, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, namespace: namespace, store: store, withTx: withTx, logger: logger}
}

// Run performs an immediate sync and then repeats every interval until ctx is cancelled.
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

// SyncOnce lists all Snapshots in the namespace and ingests any not yet stored.
func (s *Syncer) SyncOnce(ctx context.Context) {
	opts := metav1.ListOptions{Limit: 500}
	for {
		list, err := s.client.Resource(snapshotGVR).Namespace(s.namespace).List(ctx, opts)
		if err != nil {
			s.logger.Error("list snapshots", "namespace", s.namespace, "error", err)
			return
		}
		for i := range list.Items {
			if err := s.sync(ctx, &list.Items[i]); err != nil {
				s.logger.Error("ingest snapshot", "snapshot", list.Items[i].GetName(), "error", err)
			}
		}
		opts.Continue = list.GetContinue()
		if opts.Continue == "" {
			return
		}
	}
}

func (s *Syncer) sync(ctx context.Context, obj *unstructured.Unstructured) error {
	name := obj.GetName()
	exists, err := s.store.SnapshotExistsByName(ctx, name)
	if err != nil || exists {
		return err
	}

	specMap, _, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	var spec konflux.SnapshotSpec
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(specMap, &spec); err != nil {
		return fmt.Errorf("decode spec: %w", err)
	}
	if spec.Application == "" {
		s.logger.Debug("skipping snapshot without application", "snapshot", name)
		return nil
	}

	snap := konflux.Convert(spec, name)
	s.logger.Info("new snapshot", "snapshot", name, "application", snap.Application)

	return s.withTx(ctx, func(tx Store) error {
		rec, err := tx.CreateSnapshot(ctx, snap.Application, snap.Snapshot, obj.GetCreationTimestamp().UTC())
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}
		for _, c := range snap.Components {
			if _, err := tx.EnsureComponent(ctx, c.Name); err != nil {
				return fmt.Errorf("ensure component %s: %w", c.Name, err)
			}
			if err := tx.CreateSnapshotComponent(ctx, rec.ID, c.Name, c.GitRevision, c.ContainerImage, c.GitURL); err != nil {
				return fmt.Errorf("create snapshot component %s: %w", c.Name, err)
			}
		}
		return nil
	})
}
