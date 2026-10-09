package server

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

func TestProwRunEndpoints(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)

	for name, app := range map[string]string{"quay-v3.18.1": "quay-3-18", "quay-v3.18.2": "quay-3-18", "quay-v3.17.6": "quay-3-17"} {
		if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: name, KonfluxApplication: app}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func(app, name string, comps map[string]string) {
		t.Helper()
		id, err := srv.db.CreateSnapshot(ctx, app, name, t0)
		if err != nil {
			t.Fatal(err)
		}
		for comp, digest := range comps {
			if err := srv.db.CreateSnapshotComponent(ctx, id, comp, "quay.io/x/art-images@"+digest); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshot("quay-3-18", "quay-a", map[string]string{"quay-3-18-quay-quay": "sha256:aaa", "quay-3-18-quay-clair": "sha256:ccc"})
	snapshot("fbc-quay-3-18", "fbc-a", map[string]string{"fbc-quay-3-18-quay-operator": "sha256:fff"})

	seed := func(app, id string, images ...prow.Image) {
		t.Helper()
		started := t0.Add(time.Duration(id[0]-'0') * time.Hour) // newer ids start later
		run := &prow.Run{
			JobName: "job-" + app, BuildID: id, Application: app, State: "success",
			StartedAt: &started, ArtifactState: prow.ArtifactPresent, FetchedAt: started, Images: images,
		}
		if len(images) == 0 {
			run.ArtifactState = prow.ArtifactMissing
		}
		if err := srv.db.UpsertProwRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	img := func(role, digest string) prow.Image { return prow.Image{Role: role, Digest: digest} }
	seed("quay-3-18", "1", img("quay", "sha256:aaa"), img("quay", "sha256:aaa"))
	seed("quay-3-18", "2", img("quay", "sha256:old"))
	seed("quay-3-18", "3", img("catalog", "sha256:fff"))
	seed("quay-3-18", "4")
	seed("quay-3-18", "5", img("clair", "sha256:aaa")) // right digest, wrong role
	seed("quay-3-17", "6", img("quay", "sha256:aaa"))
	for _, st := range []prow.SyncState{
		{JobName: "job-quay-3-18", Application: "quay-3-18", Interval: 15 * time.Minute, LastSuccessfulSync: time.Now().UTC().Add(-time.Minute)},
		{JobName: "job-quay-3-17", Application: "quay-3-17", Interval: 15 * time.Minute, LastSuccessfulSync: t0},
	} {
		if err := srv.db.UpsertProwSync(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(runs []prow.Run) []string {
		out := []string{}
		for _, r := range runs {
			out = append(out, r.BuildID)
		}
		return out
	}

	// Every z-stream of a minor shows its application's runs.
	for _, version := range []string{"quay-v3.18.1", "quay-v3.18.2"} {
		var resp prowRunsResponse
		getJSON(t, srv, "/api/v1/releases/"+version+"/prow-runs", http.StatusOK, &resp)
		if got := ids(resp.Runs); !slices.Equal(got, []string{"5", "4", "3", "2", "1"}) {
			t.Errorf("%s runs = %v, want [5 4 3 2 1]", version, got)
		}
		if resp.Stale || resp.LastSuccessfulSync == nil {
			t.Errorf("%s sync = %v stale %v", version, resp.LastSuccessfulSync, resp.Stale)
		}
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/prow-runs", http.StatusNotFound, nil)

	var resp prowRunsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/prow-runs?limit=2&offset=1", http.StatusOK, &resp)
	if got := ids(resp.Runs); !slices.Equal(got, []string{"4", "3"}) {
		t.Errorf("page 2 = %v, want [4 3]", got)
	}
	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/prow-runs?offset=9223372036854775807", http.StatusOK, &resp)
	if len(resp.Runs) != 0 {
		t.Errorf("max offset = %v, want none", ids(resp.Runs))
	}

	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/prow-runs?unlinked=true", http.StatusOK, &resp)
	if got := ids(resp.Runs); !slices.Equal(got, []string{"5", "4", "2"}) {
		t.Errorf("unlinked = %v, want [5 4 2]", got)
	}

	var snap snapshotProwRunsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/snapshots/quay-a/prow-runs", http.StatusOK, &snap)
	if got := ids(snap.Components["quay-3-18-quay-quay"]); !slices.Equal(got, []string{"1"}) {
		t.Errorf("quay component runs = %v, want [1]", got)
	}
	if got, ok := snap.Components["quay-3-18-quay-clair"]; !ok || len(got) != 0 {
		t.Errorf("clair component runs = %v, want []", got)
	}
	snap = snapshotProwRunsResponse{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/snapshots/fbc-a/prow-runs", http.StatusOK, &snap)
	if got := ids(snap.Components["fbc-quay-3-18-quay-operator"]); !slices.Equal(got, []string{"3"}) {
		t.Errorf("catalog component runs = %v, want [3]", got)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.17.6/snapshots/quay-a/prow-runs", http.StatusNotFound, nil)

	// The 3.17 job last synced a day ago, past two 15m intervals.
	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.17.6/prow-runs", http.StatusOK, &resp)
	if !resp.Stale || resp.LastSuccessfulSync == nil || !resp.LastSuccessfulSync.Equal(t0) {
		t.Errorf("3.17 sync = %v stale %v, want %v stale", resp.LastSuccessfulSync, resp.Stale, t0)
	}
}
