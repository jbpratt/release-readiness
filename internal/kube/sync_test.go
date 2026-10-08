package kube

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const testNamespace = "art-quay-tenant"

func snapshot(name, application string, created time.Time, components ...map[string]any) *unstructured.Unstructured {
	comps := make([]any, len(components))
	for i, c := range components {
		comps[i] = c
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "appstudio.redhat.com/v1alpha1",
		"kind":       "Snapshot",
		"metadata":   map[string]any{"name": name, "namespace": testNamespace},
		"spec":       map[string]any{"application": application, "components": comps},
	}}
	u.SetCreationTimestamp(metav1.NewTime(created))
	return u
}

func component(name, image, url, revision string) map[string]any {
	return map[string]any{
		"name":           name,
		"containerImage": image,
		"source":         map[string]any{"git": map[string]any{"url": url, "revision": revision}},
	}
}

func release(name, application string, created time.Time, released map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "appstudio.redhat.com/v1alpha1",
		"kind":       "Release",
		"metadata":   map[string]any{"name": name, "namespace": testNamespace},
		"spec":       map[string]any{"snapshot": name + "-snap", "releasePlan": "plan"},
		"status": map[string]any{
			"target":     "rhtap-releng-tenant",
			"startTime":  created.Add(time.Minute).Format(time.RFC3339),
			"conditions": []any{map[string]any{"type": "Validated", "status": "True"}, released},
		},
	}}
	if application != "" {
		u.SetLabels(map[string]string{"appstudio.openshift.io/application": application})
	}
	u.SetCreationTimestamp(metav1.NewTime(created))
	return u
}

func TestSyncReleases(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	progressing := map[string]any{"type": "Released", "status": "Unknown", "reason": "Progressing"}
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		release("fbc-quay-3-18-r1", "fbc-quay-3-18", created, progressing),
		release("unlabelled-r1", "", created.Add(time.Hour), progressing),
	)
	s := NewSyncer(client, testNamespace, database, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SyncOnce(ctx)

	got, err := database.ListKonfluxReleases(ctx, "fbc-quay-3-18", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := model.KonfluxRelease{
		ID: got[0].ID, Name: "fbc-quay-3-18-r1", Application: "fbc-quay-3-18",
		Snapshot: "fbc-quay-3-18-r1-snap", ReleasePlan: "plan", Target: "rhtap-releng-tenant",
		ReleasedStatus: "Unknown", ReleasedReason: "Progressing", CreatedAt: created,
	}
	if len(got) != 1 || got[0].StartTime == nil || !got[0].StartTime.Equal(created.Add(time.Minute)) || got[0].CompletionTime != nil {
		t.Fatalf("releases = %+v", got)
	}
	got[0].StartTime = nil
	if got[0] != want {
		t.Errorf("release = %+v, want %+v", got[0], want)
	}
	if unlabelled, _ := database.ListKonfluxReleases(ctx, "", 10, 0); len(unlabelled) != 2 {
		t.Errorf("all releases = %d, want 2", len(unlabelled))
	}

	done := release("fbc-quay-3-18-r1", "fbc-quay-3-18", created,
		map[string]any{"type": "Released", "status": "True", "reason": "Succeeded"})
	completed := created.Add(10 * time.Minute)
	_ = unstructured.SetNestedField(done.Object, completed.Format(time.RFC3339), "status", "completionTime")
	if _, err := client.Resource(releaseGVR).Namespace(testNamespace).Update(ctx, done, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.SyncOnce(ctx)

	got, err = database.ListKonfluxReleases(ctx, "fbc-quay-3-18", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ReleasedStatus != "True" || got[0].ReleasedReason != "Succeeded" ||
		got[0].CompletionTime == nil || !got[0].CompletionTime.Equal(completed) {
		t.Errorf("after update = %+v", got)
	}
}

func TestSyncOnce(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	created1 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	created2 := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		snapshot("quay-3-18-abc", "quay-3-18", created1,
			component("quay", "quay.io/quay/quay@sha256:1", "https://github.com/quay/quay", "aaa"),
			component("clair", "quay.io/quay/clair@sha256:2", "https://github.com/quay/clair", "bbb")),
		snapshot("fbc-quay-3-18-def", "fbc-quay-3-18", created2,
			component("fbc", "quay.io/quay/fbc@sha256:3", "https://github.com/quay/fbc", "ccc")),
		snapshot("orphan", "", created2),
	)

	withTx := func(ctx context.Context, fn func(Store) error) error {
		return database.InTx(ctx, func(tx *db.DB) error { return fn(tx) })
	}
	s := NewSyncer(client, testNamespace, database, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SyncOnce(ctx)

	got, err := database.GetSnapshotByName(ctx, "quay-3-18-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Application != "quay-3-18" || !got.CreatedAt.Equal(created1) {
		t.Errorf("quay-3-18-abc = app %q created %v", got.Application, got.CreatedAt)
	}
	shas := map[string]string{}
	for _, c := range got.Components {
		shas[c.Component] = c.GitSHA
	}
	if len(shas) != 2 || shas["quay"] != "aaa" || shas["clair"] != "bbb" {
		t.Errorf("components = %+v", got.Components)
	}

	got, err = database.GetSnapshotByName(ctx, "fbc-quay-3-18-def")
	if err != nil {
		t.Fatal(err)
	}
	if got.Application != "fbc-quay-3-18" || !got.CreatedAt.Equal(created2) {
		t.Errorf("fbc-quay-3-18-def = app %q created %v", got.Application, got.CreatedAt)
	}

	if exists, _ := database.SnapshotExistsByName(ctx, "orphan"); exists {
		t.Error("snapshot without application was stored")
	}

	s.SyncOnce(ctx)
	for _, app := range []string{"quay-3-18", "fbc-quay-3-18"} {
		snaps, err := database.ListSnapshots(ctx, app, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(snaps) != 1 {
			t.Errorf("%s: %d snapshots after second sync, want 1", app, len(snaps))
		}
	}
}

// fakeCatalogs serves bundles per image, fails images without any, and
// records each read.
type fakeCatalogs struct {
	bundles map[string][]fbc.Bundle
	reads   []string
}

func (f *fakeCatalogs) Bundles(_ context.Context, image string) ([]fbc.Bundle, error) {
	f.reads = append(f.reads, image)
	if b, ok := f.bundles[image]; ok {
		return b, nil
	}
	return nil, errors.New("401 Unauthorized")
}

func TestSyncCatalogs(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	const (
		old    = "quay.io/x/art-fbc@sha256:01d"
		newest = "quay.io/x/art-fbc@sha256:2e3"
		cso    = "quay.io/x/art-fbc@sha256:c50"
		failed = "quay.io/x/art-fbc@sha256:bad"
	)
	t0 := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	operator := func(app, image string) map[string]any {
		return component(app+"-quay-operator", image, "https://github.com/quay/quay-operator", "abc")
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		snapshot("fbc-3-18-old", "fbc-quay-3-18", t0, operator("fbc-quay-3-18", old)),
		snapshot("fbc-3-18-new", "fbc-quay-3-18", t0.Add(time.Hour), operator("fbc-quay-3-18", newest)),
		// The newest FBC Snapshot overall is another operator's.
		snapshot("fbc-3-18-cso", "fbc-quay-3-18", t0.Add(2*time.Hour),
			component("fbc-quay-3-18-container-security-operator", cso, "", "")),
		snapshot("fbc-3-17-new", "fbc-quay-3-17", t0, operator("fbc-quay-3-17", failed)),
	)
	bundle := fbc.Bundle{Package: fbc.Package, Channel: "stable-3.18", Name: "quay-operator.v3.18.1",
		Image: "registry.redhat.io/quay/quay-operator-bundle@sha256:b24", Digest: "sha256:b24"}
	catalogs := &fakeCatalogs{bundles: map[string][]fbc.Bundle{newest: {bundle}}}
	withTx := func(ctx context.Context, fn func(Store) error) error {
		return database.InTx(ctx, func(tx *db.DB) error { return fn(tx) })
	}
	status := syncstatus.New()
	s := NewSyncer(client, testNamespace, database, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Catalogs, s.CatalogStatus = catalogs, status.Track("fbc-catalogs", 0)

	s.SyncOnce(ctx)
	if want := []string{newest, failed}; !slices.Equal(catalogs.reads, want) {
		t.Errorf("reads = %v, want %v", catalogs.reads, want)
	}
	got, err := database.LatestFBCCatalog(ctx, "fbc-quay-3-18", "fbc-quay-3-18-quay-operator", "stable-3.18")
	if err != nil {
		t.Fatal(err)
	}
	if got.Snapshot != "fbc-3-18-new" || got.State != fbc.StateParsed || !slices.Equal(got.Bundles, []fbc.Bundle{bundle}) {
		t.Errorf("3.18 catalog = %+v", got)
	}
	if got, _ := database.LatestFBCCatalog(ctx, "fbc-quay-3-17", "fbc-quay-3-17-quay-operator", "stable-3.17"); got.State != fbc.StateFailed || len(got.Bundles) != 0 {
		t.Errorf("3.17 catalog = %+v, want failed", got)
	}

	// Both are cached, the failure until its retry is due, and the failure
	// stays reported while nothing is read.
	catalogs.reads = nil
	s.SyncOnce(ctx)
	if len(catalogs.reads) != 0 {
		t.Errorf("second pass reads = %v, want none", catalogs.reads)
	}
	if p := status.Problems(time.Now()); len(p) != 1 || p[0].Source != "fbc-catalogs" {
		t.Errorf("problems = %+v, want fbc-catalogs failing", p)
	}
}

func TestNewClientKubeconfigList(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "kc.yaml")
	cfg := `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://example.invalid"}
users:
- name: u
  user: {token: t}
contexts:
- name: x
  context: {cluster: c, user: u}
current-context: x
`
	if err := os.WriteFile(kc, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(kc + string(filepath.ListSeparator) + filepath.Join(dir, "missing.yaml")); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
}
