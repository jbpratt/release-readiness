package kube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/quay/release-readiness/internal/db"
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
)

// RESTConfig loads kubeconfig, or the in-cluster service account when
// kubeconfig is empty.
func RESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		return rest.InClusterConfig()
	}
	rules := &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(kubeconfig)}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
}

// Syncer periodically ingests Konflux Snapshots and Releases from a namespace into the database.
type Syncer struct {
	client    dynamic.Interface
	namespace string
	db        *db.DB
	logger    *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
	// Retention, when set, skips unstored objects older than it that the
	// retention prune would delete.
	Retention time.Duration
}

// NewSyncer creates a Syncer that lists Snapshots in namespace and persists them to database.
func NewSyncer(client dynamic.Interface, namespace string, database *db.DB, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, namespace: namespace, db: database, logger: logger}
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

// stored is what the database holds at the start of a pass.
type stored struct {
	snapshots map[string]bool
	staged    map[string]db.StagedSnapshot
	releases  map[string]model.KonfluxRelease
	// shipped holds the Snapshots a stored Release shipped.
	shipped map[string]bool
	// cutoff is the start of the retention window, zero for none.
	cutoff time.Time
}

// SyncOnce ingests Snapshots not yet stored and upserts every Release, whose
// status keeps changing after creation, unless the stored row already holds it.
func (s *Syncer) SyncOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	st, err := s.load(ctx)
	if err != nil {
		s.logger.Error("load stored", "error", err)
		s.Status.Report(fmt.Errorf("load stored: %w", err))
		return
	}
	if s.Retention > 0 {
		st.cutoff = time.Now().Add(-s.Retention)
	}
	var pass syncstatus.Pass
	s.each(ctx, &pass, snapshotGVR, func(ctx context.Context, obj *unstructured.Unstructured) error { return s.sync(ctx, obj, st) })
	s.each(ctx, &pass, releaseGVR, func(ctx context.Context, obj *unstructured.Unstructured) error { return s.syncRelease(ctx, obj, st) })
	s.Status.Report(pass.Err())
}

func (s *Syncer) load(ctx context.Context) (*stored, error) {
	var st stored
	var err error
	if st.snapshots, err = s.db.SnapshotNames(ctx); err != nil {
		return nil, err
	}
	if st.staged, err = s.db.StagedSnapshots(ctx); err != nil {
		return nil, err
	}
	if st.releases, err = s.db.KonfluxReleases(ctx); err != nil {
		return nil, err
	}
	st.shipped = map[string]bool{}
	for _, r := range st.releases {
		if r.ReleasedReason == "Succeeded" {
			st.shipped[r.Snapshot] = true
		}
	}
	return &st, nil
}

func (s *Syncer) each(ctx context.Context, pass *syncstatus.Pass, gvr schema.GroupVersionResource, fn func(context.Context, *unstructured.Unstructured) error) {
	opts := metav1.ListOptions{Limit: 500}
	for {
		list, err := s.client.Resource(gvr).Namespace(s.namespace).List(ctx, opts)
		var status apierrors.APIStatus
		if apierrors.IsResourceExpired(err) && errors.As(err, &status) && status.Status().Continue != "" {
			// The apiserver compacts the list's revision every 5 minutes; the next pass picks up anything created mid-list.
			s.logger.Info("list continue token expired, resuming", "resource", gvr.Resource, "namespace", s.namespace)
			opts.Continue = status.Status().Continue
			continue
		}
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

func (s *Syncer) sync(ctx context.Context, obj *unstructured.Unstructured, st *stored) error {
	name := obj.GetName()
	// Checked on every pass, so Snapshots stored before this table existed get theirs.
	a := obj.GetAnnotations()
	assembly, kind, env := a["art.redhat.com/assembly"], a["art.redhat.com/kind"], a["art.redhat.com/env"]
	staged := db.StagedSnapshot{Assembly: assembly, Kind: kind, Env: env}
	annotated := assembly != "" && kind != "" && env != ""
	if annotated && st.staged[name] != staged {
		if err := s.db.UpsertStagedSnapshot(ctx, name, assembly, kind, env); err != nil {
			return fmt.Errorf("store staged snapshot: %w", err)
		}
		st.staged[name] = staged
	}
	if st.snapshots[name] {
		return nil
	}
	if !annotated && !st.shipped[name] && obj.GetCreationTimestamp().Time.Before(st.cutoff) {
		return nil
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

	return s.db.InTx(ctx, func(tx *db.DB) error {
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

func (s *Syncer) syncRelease(ctx context.Context, obj *unstructured.Unstructured, st *stored) error {
	r := &model.KonfluxRelease{
		Name:        obj.GetName(),
		Application: obj.GetLabels()["appstudio.openshift.io/application"],
		CreatedAt:   obj.GetCreationTimestamp().UTC(),
	}
	r.Snapshot, _, _ = unstructured.NestedString(obj.Object, "spec", "snapshot")
	r.ReleasePlan, _, _ = unstructured.NestedString(obj.Object, "spec", "releasePlan")
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

	old, ok := st.releases[r.Name]
	_, staged := st.staged[r.Snapshot]
	if !ok && !staged && r.ReleasedReason == "Failed" && r.CreatedAt.Before(st.cutoff) {
		return nil
	}
	if ok && (old.ReleasedReason == "Succeeded" || old.ReleasedReason == "Failed") && sameRelease(old, *r) {
		return nil
	}
	return s.db.UpsertKonfluxRelease(ctx, r)
}

func sameRelease(a, b model.KonfluxRelease) bool {
	ac, bc := a.CompletionTime, b.CompletionTime
	times := a.CreatedAt.Equal(b.CreatedAt) && (ac == nil) == (bc == nil) && (ac == nil || ac.Equal(*bc))
	a.CreatedAt, b.CreatedAt, a.CompletionTime, b.CompletionTime = time.Time{}, time.Time{}, nil, nil
	return times && a == b
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
