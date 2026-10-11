package kube

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"

	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/model"
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

func component(name, image string) map[string]any {
	return map[string]any{"name": name, "containerImage": image}
}

func release(name, application string, created time.Time, released map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "appstudio.redhat.com/v1alpha1",
		"kind":       "Release",
		"metadata":   map[string]any{"name": name, "namespace": testNamespace},
		"spec":       map[string]any{"snapshot": name + "-snap", "releasePlan": "quay-advisory-stage-3-18"},
		"status": map[string]any{
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

// releases returns application's Releases through the 3.18 STAGE ReleasePlan,
// as the candidate's stage flags read them, newest first.
func releases(t *testing.T, d *db.DB, application string) []model.KonfluxRelease {
	t.Helper()
	v := &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: application}
	staged, err := d.StageReleases(context.Background(), v, regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`))
	if err != nil {
		t.Fatal(err)
	}
	var out []model.KonfluxRelease
	for _, r := range staged {
		out = append(out, r.KonfluxRelease)
	}
	return out
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

	got := releases(t, database, "fbc-quay-3-18")
	want := model.KonfluxRelease{
		Name: "fbc-quay-3-18-r1", Application: "fbc-quay-3-18", Snapshot: "fbc-quay-3-18-r1-snap", ReleasePlan: "quay-advisory-stage-3-18",
		ReleasedStatus: "Unknown", ReleasedReason: "Progressing", CreatedAt: created,
	}
	if len(got) != 1 || got[0].StartTime == nil || !got[0].StartTime.Equal(created.Add(time.Minute)) || got[0].CompletionTime != nil {
		t.Fatalf("releases = %+v", got)
	}
	got[0].StartTime = nil
	if got[0] != want {
		t.Errorf("release = %+v, want %+v", got[0], want)
	}
	// The prod Release is found by Snapshot name alone, so an unlabelled one is stored too.
	if err := database.UpsertStagedSnapshot(ctx, "unlabelled-r1-snap", "3.18.1", "image", "prod", created); err != nil {
		t.Fatal(err)
	}
	if prod, err := database.LatestProdRelease(ctx, "3.18.1"); err != nil || prod == nil || prod.Name != "unlabelled-r1" {
		t.Errorf("prod = %+v, %v; want Release unlabelled-r1", prod, err)
	}

	done := release("fbc-quay-3-18-r1", "fbc-quay-3-18", created,
		map[string]any{"type": "Released", "status": "True", "reason": "Succeeded"})
	completed := created.Add(10 * time.Minute)
	_ = unstructured.SetNestedField(done.Object, completed.Format(time.RFC3339), "status", "completionTime")
	if _, err := client.Resource(releaseGVR).Namespace(testNamespace).Update(ctx, done, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.SyncOnce(ctx)

	got = releases(t, database, "fbc-quay-3-18")
	if len(got) != 1 || got[0].ReleasedStatus != "True" || got[0].ReleasedReason != "Succeeded" ||
		got[0].CompletionTime == nil || !got[0].CompletionTime.Equal(completed) {
		t.Errorf("after update = %+v", got)
	}
}

func TestSyncReleaseFailedTask(t *testing.T) {
	failed := map[string]any{"type": "Released", "status": "False", "reason": "Failed"}
	attempt := func(task, step string) any {
		return map[string]any{"lastTask": task, "lastStep": step, "failureReason": "Error"}
	}
	for _, tc := range []struct {
		name     string
		released map[string]any
		attempts []any
		task     string
		step     string
	}{
		{"apply-mapping", failed, []any{attempt("apply-mapping", "apply-mapping")}, "apply-mapping", "apply-mapping"},
		{"access", failed, []any{attempt("verify-access-to-resources", "")}, "verify-access-to-resources", ""},
		{"conforma last attempt", failed, []any{attempt("apply-mapping", "apply-mapping"), attempt("verify-conforma", "assert")}, "verify-conforma", "assert"},
		{"succeeded", map[string]any{"type": "Released", "status": "True", "reason": "Succeeded"}, []any{attempt("collect-data", "x")}, "", ""},
		{"no attempts", failed, nil, "", ""},
		{"progressing after failed attempt", map[string]any{"type": "Released", "status": "False", "reason": "Progressing"}, []any{attempt("verify-conforma", "assert")}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			r := release("r1", "quay-3-18", time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC), tc.released)
			if tc.attempts != nil {
				_ = unstructured.SetNestedSlice(r.Object, tc.attempts, "status", "managedPipelineAttempts")
			}
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
				map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"}, r)
			NewSyncer(client, testNamespace, database, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).SyncOnce(ctx)

			got := releases(t, database, "quay-3-18")
			if len(got) != 1 {
				t.Fatalf("releases = %+v", got)
			}
			if got[0].FailedTask != tc.task || got[0].FailedStep != tc.step {
				t.Errorf("failed = %q/%q, want %q/%q", got[0].FailedTask, got[0].FailedStep, tc.task, tc.step)
			}
		})
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
			component("quay", "quay.io/quay/quay@sha256:1"),
			component("clair", "quay.io/quay/clair@sha256:2")),
		snapshot("fbc-quay-3-18-def", "fbc-quay-3-18", created2,
			component("fbc", "quay.io/quay/fbc@sha256:3")),
		snapshot("orphan", "", created2),
	)

	withTx := func(ctx context.Context, fn func(Store) error) error {
		return database.InTx(ctx, func(tx *db.DB) error { return fn(tx) })
	}
	s := NewSyncer(client, testNamespace, database, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SyncOnce(ctx)

	got, err := database.GetReleaseSnapshot(ctx, "quay-3-18-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Application != "quay-3-18" || !got.CreatedAt.Equal(created1) {
		t.Errorf("quay-3-18-abc = app %q created %v", got.Application, got.CreatedAt)
	}
	images := map[string]string{}
	for _, c := range got.Components {
		images[c.Name] = c.Image
	}
	if len(images) != 2 || images["quay"] != "quay.io/quay/quay@sha256:1" || images["clair"] != "quay.io/quay/clair@sha256:2" {
		t.Errorf("components = %+v", got.Components)
	}

	got, err = database.GetReleaseSnapshot(ctx, "fbc-quay-3-18-def")
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
	for name, want := range map[string]int{"quay-3-18-abc": 2, "fbc-quay-3-18-def": 1} {
		got, err := database.GetReleaseSnapshot(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Components) != want {
			t.Errorf("%s: %d components after second sync, want %d", name, len(got.Components), want)
		}
	}
}

func TestSyncStagedSnapshots(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	annotated := func(name string, created time.Time, env string) *unstructured.Unstructured {
		u := snapshot(name, "quay-3-18", created)
		u.SetAnnotations(map[string]string{
			"art.redhat.com/assembly": "3.18.1",
			"art.redhat.com/kind":     "image",
			"art.redhat.com/env":      env,
		})
		return u
	}
	// Newer Snapshots that are not staged must not be picked.
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		annotated("quay-stage-3-18-1-image", t0, "stage"),
		annotated("quay-prod-3-18-1-image", t0.Add(time.Hour), "prod"),
		snapshot("quay-3-18-build", "quay-3-18", t0.Add(2*time.Hour)),
	)
	// Stored before staged Snapshots were recorded: the sync still records it.
	if _, err := database.CreateSnapshot(ctx, "quay-3-18", "quay-stage-3-18-1-image", t0); err != nil {
		t.Fatal(err)
	}
	withTx := func(ctx context.Context, fn func(Store) error) error {
		return database.InTx(ctx, func(tx *db.DB) error { return fn(tx) })
	}
	NewSyncer(client, testNamespace, database, withTx, slog.New(slog.NewTextHandler(io.Discard, nil))).SyncOnce(ctx)

	// Each Snapshot's Release succeeded through the STAGE plan, the newest
	// last: only the one annotated for stage is selected.
	for i, snap := range []string{"quay-stage-3-18-1-image", "quay-prod-3-18-1-image", "quay-3-18-build"} {
		done := t0.Add(time.Duration(3+i) * time.Hour)
		if err := database.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: snap, Application: "quay-3-18", Snapshot: snap, ReleasePlan: "quay-advisory-stage-3-18",
			ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: t0, CompletionTime: &done,
		}); err != nil {
			t.Fatal(err)
		}
	}
	v := &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}
	got, _, err := database.SelectedStageBuild(ctx, v, regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SnapshotName != "quay-stage-3-18-1-image" {
		t.Errorf("selected = %+v, want quay-stage-3-18-1-image", got)
	}
}

func TestRESTConfigKubeconfigList(t *testing.T) {
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
	rc, err := RESTConfig(kc + string(filepath.ListSeparator) + filepath.Join(dir, "missing.yaml"))
	if err != nil || rc.BearerToken != "t" {
		t.Fatalf("RESTConfig: %+v, %v; want the token t", rc, err)
	}
}

func TestNewClientError(t *testing.T) {
	c, err := NewClient(&rest.Config{Host: "https://example.invalid", TLSClientConfig: rest.TLSClientConfig{CAData: []byte("not PEM")}})
	if err == nil || c != nil {
		t.Fatalf("NewClient = %#v, %v; want a nil client and an error", c, err)
	}
}
