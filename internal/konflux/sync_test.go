package konflux

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/kube"
)

const snapshotsJSON = `{
	"metadata": {},
	"items": [
		{
			"metadata": {"name": "existing-snap", "creationTimestamp": "2026-01-01T00:00:00Z"},
			"spec": {"application": "app1", "components": []}
		},
		{
			"metadata": {"name": "app1-snap-a", "creationTimestamp": "2026-01-01T00:00:00Z"},
			"spec": {"application": "app1", "components": []}
		},
		{
			"metadata": {"name": "app1-snap-b", "creationTimestamp": "2026-01-01T00:00:00Z"},
			"spec": {"application": "app1", "components": []}
		},
		{
			"metadata": {"name": "app2-comp-snap", "creationTimestamp": "2026-01-02T00:00:00Z"},
			"spec": {
				"application": "app2",
				"components": [
					{"name": "comp-a", "containerImage": "quay.io/a/a@sha256:aaa", "source": {"git": {"url": "https://github.com/a/a", "revision": "deadbeef"}}}
				]
			}
		},
		{
			"metadata": {"name": "app2-empty-snap", "creationTimestamp": "2026-01-02T00:00:00Z"},
			"spec": {"application": "app2", "components": []}
		}
	]
}`

func newTestSyncer(t *testing.T, database *db.DB, responseJSON string) *Syncer {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/appstudio.redhat.com/v1alpha1/namespaces/ns/snapshots" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(responseJSON))
	}))
	t.Cleanup(srv.Close)

	client, err := kube.NewClient(&kube.Config{Server: srv.URL, BearerToken: "fake-token", InsecureSkipTLSVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	tx := func(ctx context.Context, fn func(Store) error) error {
		return database.InTx(ctx, func(txDB *db.DB) error {
			return fn(txDB)
		})
	}
	return NewSyncer(client, "ns", database, tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSyncOnce(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.CreateSnapshot(ctx, "app1", "existing-snap", false, time.Now()); err != nil {
		t.Fatal(err)
	}

	newTestSyncer(t, database, snapshotsJSON).syncOnce(ctx)

	tests := []struct {
		name        string
		wantExists  bool
		testsPassed bool
	}{
		{"existing-snap", true, false}, // pre-existing row is left untouched
		// Konflux test status is not read, so every new snapshot is stored as not passed.
		{"app1-snap-a", true, false},
		{"app1-snap-b", true, false},
		{"app2-comp-snap", true, false},
		{"app2-empty-snap", true, false},
	}
	for _, tt := range tests {
		exists, err := database.SnapshotExistsByName(ctx, tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if exists != tt.wantExists {
			t.Errorf("%s: exists = %v, want %v", tt.name, exists, tt.wantExists)
			continue
		}
		if !exists {
			continue
		}
		snap, err := database.GetSnapshotByName(ctx, tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if snap.TestsPassed != tt.testsPassed {
			t.Errorf("%s: TestsPassed = %v, want %v", tt.name, snap.TestsPassed, tt.testsPassed)
		}
	}

	withComps, err := database.GetSnapshotByName(ctx, "app2-comp-snap")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !withComps.CreatedAt.Equal(want) {
		t.Errorf("app2-comp-snap: CreatedAt = %v, want %v", withComps.CreatedAt, want)
	}
	if len(withComps.Components) != 1 || withComps.Components[0].Component != "comp-a" || withComps.Components[0].GitSHA != "deadbeef" {
		t.Errorf("app2-comp-snap: Components = %+v, want one comp-a at deadbeef", withComps.Components)
	}

	components, err := database.ListComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].Name != "comp-a" {
		t.Errorf("components table = %+v, want one row comp-a", components)
	}
}

// TestSyncOnce_LatestByCreationTime guards against inserting snapshots in
// list-API order: the list below returns app-a-snap first (name order), but
// app-a-snap is the newer one, so a naive insert order would make the older
// app-z-snap "latest" by id.
func TestSyncOnce_LatestByCreationTime(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	const reorderedJSON = `{
		"metadata": {},
		"items": [
			{
				"metadata": {"name": "app-a-snap", "creationTimestamp": "2026-10-06T00:00:00Z"},
				"spec": {"application": "app", "components": []}
			},
			{
				"metadata": {"name": "app-z-snap", "creationTimestamp": "2026-09-01T00:00:00Z"},
				"spec": {"application": "app", "components": []}
			}
		]
	}`
	newTestSyncer(t, database, reorderedJSON).syncOnce(ctx)

	summaries, err := database.LatestSnapshotPerApplication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].LatestSnapshot == nil {
		t.Fatalf("LatestSnapshotPerApplication = %+v, want one application", summaries)
	}
	if got := summaries[0].LatestSnapshot.Name; got != "app-a-snap" {
		t.Errorf("LatestSnapshotPerApplication latest = %q, want app-a-snap (the newer snapshot)", got)
	}
}
