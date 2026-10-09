package kube

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

var (
	snapshotGVR = schema.GroupVersionResource{Group: "appstudio.redhat.com", Version: "v1alpha1", Resource: "snapshots"}
	releaseGVR  = schema.GroupVersionResource{Group: "appstudio.redhat.com", Version: "v1alpha1", Resource: "releases"}
)

const (
	// syncTimeout bounds one sync so a hung API call cannot stall the loop.
	syncTimeout = 5 * time.Minute
	// fbcRetryAfter spaces out reads of a catalog image that failed.
	fbcRetryAfter = 15 * time.Minute
	fbcPerPass    = 5
)

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
	CreateSnapshot(ctx context.Context, application, name string, createdAt time.Time) (id int64, err error)
	CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, imageURL string) error
	UpsertKonfluxRelease(ctx context.Context, r *model.KonfluxRelease) error
	UpsertStagedSnapshot(ctx context.Context, name, assembly, kind, env string, createdAt time.Time) error
	ListFBCCatalogCandidates(ctx context.Context, retryBefore time.Time, limit int) ([]string, error)
	// ReplaceFBCCatalog atomically stores the outcome of reading one catalog image.
	ReplaceFBCCatalog(ctx context.Context, digest, state string, bundles []fbc.Bundle, checkedAt time.Time) error
}

// CatalogReader reads the bundles of a digest-pinned FBC image.
type CatalogReader interface {
	Bundles(ctx context.Context, image string) ([]fbc.Bundle, error)
}

// TxFunc wraps a function in a database transaction, passing a tx-scoped Store.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Syncer periodically ingests Konflux Snapshots and Releases from a namespace into a Store.
type Syncer struct {
	client    dynamic.Interface
	namespace string
	store     Store
	withTx    TxFunc
	logger    *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
	// Catalogs, when set, reads new quay-operator FBC catalogs after each
	// pass and reports to CatalogStatus.
	Catalogs      CatalogReader
	CatalogStatus *syncstatus.Source
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

// SyncOnce ingests Snapshots not yet stored and upserts every Release, whose
// status keeps changing after creation.
func (s *Syncer) SyncOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	var pass syncstatus.Pass
	s.each(ctx, &pass, snapshotGVR, s.sync)
	s.each(ctx, &pass, releaseGVR, s.syncRelease)
	s.Status.Report(pass.Err())
	if s.Catalogs != nil {
		s.syncCatalogs(ctx)
	}
}

// syncCatalogs reads the catalogs of the newest quay-operator FBC images not
// yet read. A failed read is stored so the API reports unknown, not a stale
// catalog, and is retried after fbcRetryAfter. A pass with nothing to read
// leaves the last outcome standing.
func (s *Syncer) syncCatalogs(ctx context.Context) {
	now := time.Now().UTC()
	images, err := s.store.ListFBCCatalogCandidates(ctx, now.Add(-fbcRetryAfter), fbcPerPass)
	if err != nil {
		s.logger.Error("list fbc catalog candidates", "error", err)
		s.CatalogStatus.Report(fmt.Errorf("list catalog candidates: %w", err))
		return
	}
	if len(images) == 0 {
		return
	}
	var pass syncstatus.Pass
	for _, img := range images {
		_, digest, _ := strings.Cut(img, "@")
		state := fbc.StateParsed
		bundles, err := s.Catalogs.Bundles(ctx, img)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Warn("read fbc catalog", "image", img, "error", err)
			pass.Add(fmt.Errorf("read catalog %s: %w", img, err))
			state, bundles = fbc.StateFailed, nil
		}
		if err := s.store.ReplaceFBCCatalog(ctx, digest, state, bundles, now); err != nil {
			s.logger.Error("store fbc catalog", "image", img, "error", err)
			pass.Add(fmt.Errorf("store catalog %s: %w", img, err))
			continue
		}
		s.logger.Info("read fbc catalog", "image", img, "state", state, "bundles", len(bundles))
	}
	s.CatalogStatus.Report(pass.Err())
}

func (s *Syncer) each(ctx context.Context, pass *syncstatus.Pass, gvr schema.GroupVersionResource, fn func(context.Context, *unstructured.Unstructured) error) {
	opts := metav1.ListOptions{Limit: 500}
	for {
		list, err := s.client.Resource(gvr).Namespace(s.namespace).List(ctx, opts)
		if err != nil {
			s.logger.Error("list", "resource", gvr.Resource, "namespace", s.namespace, "error", err)
			pass.Add(fmt.Errorf("list %s: %w", gvr.Resource, err))
			return
		}
		for i := range list.Items {
			if err := fn(ctx, &list.Items[i]); err != nil {
				s.logger.Error("ingest", "resource", gvr.Resource, "name", list.Items[i].GetName(), "error", err)
				pass.Add(fmt.Errorf("ingest %s %s: %w", gvr.Resource, list.Items[i].GetName(), err))
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
	// Recorded on every pass, so Snapshots stored before this table existed get theirs.
	a := obj.GetAnnotations()
	assembly, kind, env := a["art.redhat.com/assembly"], a["art.redhat.com/kind"], a["art.redhat.com/env"]
	if assembly != "" && kind != "" && env != "" {
		if err := s.store.UpsertStagedSnapshot(ctx, name, assembly, kind, env, obj.GetCreationTimestamp().UTC()); err != nil {
			return fmt.Errorf("store staged snapshot: %w", err)
		}
	}
	exists, err := s.store.SnapshotExistsByName(ctx, name)
	if err != nil || exists {
		return err
	}

	specMap, _, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	var spec struct {
		Application string `json:"application"`
		Components  []struct {
			Name           string `json:"name"`
			ContainerImage string `json:"containerImage"`
		} `json:"components"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(specMap, &spec); err != nil {
		return fmt.Errorf("decode spec: %w", err)
	}
	if spec.Application == "" {
		s.logger.Debug("skipping snapshot without application", "snapshot", name)
		return nil
	}

	s.logger.Info("new snapshot", "snapshot", name, "application", spec.Application)

	return s.withTx(ctx, func(tx Store) error {
		id, err := tx.CreateSnapshot(ctx, spec.Application, name, obj.GetCreationTimestamp().UTC())
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}
		for _, c := range spec.Components {
			if err := tx.CreateSnapshotComponent(ctx, id, c.Name, c.ContainerImage); err != nil {
				return fmt.Errorf("create snapshot component %s: %w", c.Name, err)
			}
		}
		return nil
	})
}

func (s *Syncer) syncRelease(ctx context.Context, obj *unstructured.Unstructured) error {
	r := &model.KonfluxRelease{
		Name:        obj.GetName(),
		Application: obj.GetLabels()["appstudio.openshift.io/application"],
		CreatedAt:   obj.GetCreationTimestamp().UTC(),
	}
	r.Snapshot, _, _ = unstructured.NestedString(obj.Object, "spec", "snapshot")
	r.ReleasePlan, _, _ = unstructured.NestedString(obj.Object, "spec", "releasePlan")
	r.StartTime = nestedTime(obj, "status", "startTime")
	r.CompletionTime = nestedTime(obj, "status", "completionTime")

	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok || cond["type"] != "Released" {
			continue
		}
		r.ReleasedStatus, _ = cond["status"].(string)
		r.ReleasedReason, _ = cond["reason"].(string)
	}
	// Released=False with reason Progressing is a running Release; only a
	// Failed one has a task it stopped at.
	if r.ReleasedReason == "Failed" {
		attempts, _, _ := unstructured.NestedSlice(obj.Object, "status", "managedPipelineAttempts")
		if n := len(attempts); n > 0 {
			last, _ := attempts[n-1].(map[string]any)
			r.FailedTask, _ = last["lastTask"].(string)
			r.FailedStep, _ = last["lastStep"].(string)
		}
	}

	return s.store.UpsertKonfluxRelease(ctx, r)
}

func nestedTime(obj *unstructured.Unstructured, fields ...string) *time.Time {
	v, _, _ := unstructured.NestedString(obj.Object, fields...)
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}
