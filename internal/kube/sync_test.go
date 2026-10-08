package kube

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/quay/release-readiness/internal/db"
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
		map[schema.GroupVersionResource]string{snapshotGVR: "SnapshotList"},
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
	if got.Application != "quay-3-18" || !got.CreatedAt.Equal(created1) || got.TestsPassed {
		t.Errorf("quay-3-18-abc = app %q created %v passed %v", got.Application, got.CreatedAt, got.TestsPassed)
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
