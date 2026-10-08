package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/prow"
)

func TestProwRunEndpoints(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	seed := func(job, id, release string, started time.Time, digest string) {
		t.Helper()
		run := &prow.Run{
			JobName: job, BuildID: id, Kind: prow.KindPeriodic, ReleaseVersion: release, State: "success",
			StartedAt: &started, ArtifactState: prow.ArtifactPresent, FetchedAt: started,
			Images: []prow.Image{{Role: "quay", Source: "pod", RequestedRef: "registry.redhat.io/quay/quay-rhel9@" + digest, Digest: digest}},
		}
		if err := srv.db.UpsertProwRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	seed("p318", "1", "quay-v3.18.0", t0, "sha256:old")
	seed("p318", "2", "quay-v3.18.0", t0.Add(time.Hour), "sha256:shared")
	seed("p317", "3", "quay-v3.17.0", t0.Add(2*time.Hour), "sha256:shared")
	for _, st := range []prow.SyncState{
		{JobName: "p318", ReleaseVersion: "quay-v3.18.0", Interval: 15 * time.Minute, LastSuccessfulSync: time.Now().UTC().Add(-time.Minute)},
		{JobName: "p317", ReleaseVersion: "quay-v3.17.0", Interval: 15 * time.Minute, LastSuccessfulSync: t0},
	} {
		if err := srv.db.UpsertProwSync(ctx, st); err != nil {
			t.Fatal(err)
		}
	}

	ids := func(resp prowRunsResponse) []string {
		var out []string
		for _, r := range resp.Runs {
			out = append(out, r.BuildID)
		}
		return out
	}

	var resp prowRunsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/prow-runs", http.StatusOK, &resp)
	if got := ids(resp); len(got) != 2 || got[0] != "2" || got[1] != "1" {
		t.Errorf("3.18 runs = %v, want [2 1]", got)
	}
	if resp.Stale || resp.LastSuccessfulSync == nil || len(resp.Runs[0].Images) != 1 || resp.Runs[0].Images[0].Digest != "sha256:shared" {
		t.Errorf("3.18 response = %+v", resp)
	}

	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/prow-runs?limit=1&offset=1", http.StatusOK, &resp)
	if got := ids(resp); len(got) != 1 || got[0] != "1" {
		t.Errorf("3.18 page 2 = %v, want [1]", got)
	}

	// p317 last synced a day ago, past two 15m intervals.
	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/prow-runs/by-digest/sha256:shared", http.StatusOK, &resp)
	if got := ids(resp); len(got) != 2 || got[0] != "3" || got[1] != "2" {
		t.Errorf("by-digest runs = %v, want [3 2]", got)
	}
	if !resp.Stale || resp.LastSuccessfulSync == nil || !resp.LastSuccessfulSync.Equal(t0) {
		t.Errorf("by-digest sync = %v stale %v, want %v stale", resp.LastSuccessfulSync, resp.Stale, t0)
	}

	resp = prowRunsResponse{}
	getJSON(t, srv, "/api/v1/prow-runs/by-digest/sha256:none", http.StatusOK, &resp)
	if resp.Runs == nil || len(resp.Runs) != 0 || resp.LastSuccessfulSync != nil {
		t.Errorf("unknown digest = %+v, want empty runs", resp)
	}
}
