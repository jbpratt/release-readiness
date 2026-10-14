package kube

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"

	"github.com/quay/release-readiness/internal/db"
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
	s := NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SyncOnce(ctx)

	got := releases(t, database, "fbc-quay-3-18")
	want := model.KonfluxRelease{
		Name: "fbc-quay-3-18-r1", Application: "fbc-quay-3-18", Snapshot: "fbc-quay-3-18-r1-snap", ReleasePlan: "quay-advisory-stage-3-18",
		ReleasedStatus: "Unknown", ReleasedReason: "Progressing", CreatedAt: created,
	}
	if len(got) != 1 || got[0].CompletionTime != nil {
		t.Fatalf("releases = %+v", got)
	}
	if got[0] != want {
		t.Errorf("release = %+v, want %+v", got[0], want)
	}
	// The prod Release is found by Snapshot name alone, so an unlabelled one is stored too.
	if err := database.UpsertStagedSnapshot(ctx, "unlabelled-r1-snap", "3.18.1", "image", "prod"); err != nil {
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
			NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil))).SyncOnce(ctx)

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

	s := NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Each Snapshot is stored once, under its application, with its images.
	check := func() {
		t.Helper()
		for app, want := range map[string]model.ReleaseSnapshot{
			"quay-3-18": {Name: "quay-3-18-abc", CreatedAt: created1, Components: []model.SnapshotImage{
				{Name: "clair", Image: "quay.io/quay/clair@sha256:2"}, {Name: "quay", Image: "quay.io/quay/quay@sha256:1"},
			}},
			"fbc-quay-3-18": {Name: "fbc-quay-3-18-def", CreatedAt: created2, Components: []model.SnapshotImage{{Name: "fbc", Image: "quay.io/quay/fbc@sha256:3"}}},
		} {
			if got, _, err := database.StreamBuilds(ctx, app); err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Errorf("%s snapshots = %+v, %v; want %+v", app, got, err, want)
			}
		}
	}
	s.SyncOnce(ctx)
	check()
	if names, _ := database.SnapshotNames(ctx); names["orphan"] {
		t.Error("snapshot without application was stored")
	}
	s.SyncOnce(ctx)
	check()
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
	NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil))).SyncOnce(ctx)

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
	got, err := database.SelectedStageBuild(ctx, v, regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SnapshotName != "quay-stage-3-18-1-image" {
		t.Errorf("selected = %+v, want quay-stage-3-18-1-image", got)
	}
}

// TestSyncSkipsUnchanged counts each pass's writes: over the same objects,
// only a Release still running is written again.
func TestSyncSkipsUnchanged(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	staged := func(name, assembly string) *unstructured.Unstructured {
		u := snapshot(name, "quay-3-18", created, component("quay", "quay.io/quay/quay@sha256:1"))
		u.SetAnnotations(map[string]string{"art.redhat.com/assembly": assembly, "art.redhat.com/kind": "image", "art.redhat.com/env": "stage"})
		return u
	}
	completed := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		_ = unstructured.SetNestedField(u.Object, created.Add(10*time.Minute).Format(time.RFC3339), "status", "completionTime")
		return u
	}
	succeeded := map[string]any{"type": "Released", "status": "True", "reason": "Succeeded"}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		staged("snap-a", "3.18.1"),
		staged("snap-b", "3.18.1"),
		snapshot("snap-build", "quay-3-18", created),
		completed(release("r-succeeded", "quay-3-18", created, succeeded)),
		completed(release("r-failed", "quay-3-18", created, map[string]any{"type": "Released", "status": "False", "reason": "Failed"})),
		release("r-progressing", "quay-3-18", created, map[string]any{"type": "Released", "status": "Unknown", "reason": "Progressing"}),
	)
	s := NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil)))

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.Exec(`CREATE TABLE writes (w TEXT);
CREATE TRIGGER w1 AFTER INSERT ON snapshots BEGIN INSERT INTO writes VALUES ('snapshots ' || NEW.name); END;
CREATE TRIGGER w2 AFTER INSERT ON staged_snapshots BEGIN INSERT INTO writes VALUES ('staged_snapshots ' || NEW.name); END;
CREATE TRIGGER w3 AFTER UPDATE ON staged_snapshots BEGIN INSERT INTO writes VALUES ('staged_snapshots ' || NEW.name); END;
CREATE TRIGGER w4 AFTER INSERT ON konflux_releases BEGIN INSERT INTO writes VALUES ('konflux_releases ' || NEW.name); END;
CREATE TRIGGER w5 AFTER UPDATE ON konflux_releases BEGIN INSERT INTO writes VALUES ('konflux_releases ' || NEW.name); END;`); err != nil {
		t.Fatal(err)
	}
	pass := func(want ...string) {
		t.Helper()
		s.SyncOnce(ctx)
		var got []string
		rows, err := raw.Query(`DELETE FROM writes RETURNING w`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var w string
			_ = rows.Scan(&w)
			got = append(got, w)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("writes = %q, want %q", got, want)
		}
	}

	pass("konflux_releases r-failed", "konflux_releases r-progressing", "konflux_releases r-succeeded",
		"snapshots snap-a", "snapshots snap-b", "snapshots snap-build",
		"staged_snapshots snap-a", "staged_snapshots snap-b")
	pass("konflux_releases r-progressing")

	// A retried Release succeeds, and ART restages a Snapshot.
	if _, err := client.Resource(releaseGVR).Namespace(testNamespace).Update(ctx, completed(release("r-failed", "quay-3-18", created, succeeded)), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(snapshotGVR).Namespace(testNamespace).Update(ctx, staged("snap-b", "3.18.2"), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pass("konflux_releases r-failed", "konflux_releases r-progressing", "staged_snapshots snap-b")
	stored, err := database.KonfluxReleases(ctx)
	if err != nil || stored["r-failed"].ReleasedReason != "Succeeded" {
		t.Errorf("r-failed = %+v, %v; want Succeeded", stored["r-failed"], err)
	}
	if st, err := database.StagedSnapshots(ctx); err != nil || st["snap-b"].Assembly != "3.18.2" {
		t.Errorf("snap-b = %+v, %v; want assembly 3.18.2", st["snap-b"], err)
	}
}

// TestSyncListContinueExpired serves the second page of Snapshots as the 410
// the apiserver returns once it has compacted the list's revision.
func TestSyncListContinueExpired(t *testing.T) {
	for _, tc := range []struct {
		name      string
		expired   string   // the 410 body for continue=t1
		continues []string // the continue parameter of each Snapshot list
		stored    []string
		problem   string // the konflux problem's message prefix, "" for none
	}{
		{
			name:      "token",
			expired:   `{"kind":"Status","apiVersion":"v1","metadata":{"continue":"t2"},"status":"Failure","reason":"Expired","code":410}`,
			continues: []string{"", "t1", "t2"},
			stored:    []string{"quay-3-18-a", "quay-3-18-b"},
		},
		{
			name:      "no token",
			expired:   `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Expired","code":410}`,
			continues: []string{"", "t1"},
			stored:    []string{"quay-3-18-a"},
			problem:   "list snapshots",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			pages := map[string]string{
				"":   `{"apiVersion":"appstudio.redhat.com/v1alpha1","kind":"SnapshotList","metadata":{"continue":"t1"},"items":[{"metadata":{"name":"quay-3-18-a"},"spec":{"application":"quay-3-18"}}]}`,
				"t2": `{"apiVersion":"appstudio.redhat.com/v1alpha1","kind":"SnapshotList","metadata":{},"items":[{"metadata":{"name":"quay-3-18-b"},"spec":{"application":"quay-3-18"}}]}`,
			}
			var continues []string
			mux := http.NewServeMux()
			mux.HandleFunc("GET /apis/appstudio.redhat.com/v1alpha1/namespaces/art-quay-tenant/snapshots", func(w http.ResponseWriter, r *http.Request) {
				c := r.URL.Query().Get("continue")
				continues = append(continues, c)
				w.Header().Set("Content-Type", "application/json")
				if c == "t1" {
					w.WriteHeader(http.StatusGone)
					_, _ = w.Write([]byte(tc.expired))
					return
				}
				_, _ = w.Write([]byte(pages[c]))
			})
			mux.HandleFunc("GET /apis/appstudio.redhat.com/v1alpha1/namespaces/art-quay-tenant/releases", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"apiVersion":"appstudio.redhat.com/v1alpha1","kind":"ReleaseList","metadata":{},"items":[]}`))
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			client, err := dynamic.NewForConfig(&rest.Config{Host: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			reg := syncstatus.New()
			s := NewSyncer(client, testNamespace, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
			s.Status = reg.Track("konflux", 0)
			s.SyncOnce(ctx)

			if !slices.Equal(continues, tc.continues) {
				t.Errorf("continue parameters = %q, want %q", continues, tc.continues)
			}
			names, _ := database.SnapshotNames(ctx)
			var stored []string
			for _, name := range []string{"quay-3-18-a", "quay-3-18-b"} {
				if names[name] {
					stored = append(stored, name)
				}
			}
			if !slices.Equal(stored, tc.stored) {
				t.Errorf("stored = %q, want %q", stored, tc.stored)
			}
			p := reg.Problems(time.Now())
			if tc.problem == "" && len(p) != 0 || tc.problem != "" && (len(p) != 1 || p[0].Source != "konflux" || !strings.HasPrefix(p[0].Message, tc.problem)) {
				t.Errorf("problems = %+v, want konflux %q", p, tc.problem)
			}
		})
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

// With retention set, an unstored Snapshot older than it is skipped unless ART
// staged it or a stored Release shipped it, and so is an unstored Failed
// Release older than it unless its Snapshot is staged. Without retention every
// one is ingested.
func TestSyncRetention(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	old, young := time.Now().Add(-48*time.Hour).UTC().Truncate(time.Second), time.Now().UTC().Truncate(time.Second)
	if err := database.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
		Name: "shipped-r1", Application: "quay-3-18", Snapshot: "old-shipped", ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	staged := snapshot("old-staged", "quay-3-18", old)
	staged.SetAnnotations(map[string]string{"art.redhat.com/assembly": "3.18.1", "art.redhat.com/kind": "image", "art.redhat.com/env": "stage"})
	failed := map[string]any{"type": "Released", "status": "False", "reason": "Failed"}
	stagedFailed := release("staged-failed", "quay-3-18", old, failed)
	if err := unstructured.SetNestedField(stagedFailed.Object, "old-staged", "spec", "snapshot"); err != nil {
		t.Fatal(err)
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList", releaseGVR: "ReleaseList"},
		snapshot("old", "quay-3-18", old),
		staged,
		snapshot("old-shipped", "quay-3-18", old),
		snapshot("young", "quay-3-18", young),
		release("old-failed", "quay-3-18", old, failed),
		release("old-succeeded", "quay-3-18", old, map[string]any{"type": "Released", "status": "True", "reason": "Succeeded"}),
		release("young-failed", "quay-3-18", young, failed),
		stagedFailed,
	)
	s := NewSyncer(client, testNamespace, database, slog.New(slog.DiscardHandler))
	s.Retention = 24 * time.Hour
	s.SyncOnce(ctx)

	stored := func() []string {
		t.Helper()
		snaps, err := database.SnapshotNames(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rels, err := database.KonfluxReleases(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for n := range snaps {
			names = append(names, n)
		}
		for n := range rels {
			names = append(names, n)
		}
		slices.Sort(names)
		return names
	}
	if got, want := stored(), []string{"old-shipped", "old-staged", "old-succeeded", "shipped-r1", "staged-failed", "young", "young-failed"}; !slices.Equal(got, want) {
		t.Errorf("stored with retention = %v, want %v", got, want)
	}

	s.Retention = 0
	s.SyncOnce(ctx)
	if got, want := stored(), []string{"old", "old-failed", "old-shipped", "old-staged", "old-succeeded", "shipped-r1", "staged-failed", "young", "young-failed"}; !slices.Equal(got, want) {
		t.Errorf("stored without retention = %v, want %v", got, want)
	}
}
