package server

import (
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/github"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		_ = os.Remove(dbPath)
	})
	return New(database, ":0", "https://redhat.atlassian.net", "PROJQUAY", "https://art.example", nil, syncstatus.New(), slog.Default())
}

func TestHealthEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("status: got %q, want healthy", resp["status"])
	}
}

func TestConfigJiraEnabled(t *testing.T) {
	srv := setupTestServer(t)
	var got map[string]any
	getJSON(t, srv, "/api/v1/config", http.StatusOK, &got)
	if got["jira_enabled"] != false {
		t.Errorf("without a jira sync: jira_enabled = %v, want false", got["jira_enabled"])
	}

	srv.syncStatus.Track("jira", time.Minute)
	getJSON(t, srv, "/api/v1/config", http.StatusOK, &got)
	if got["jira_enabled"] != true {
		t.Errorf("with a jira sync: jira_enabled = %v, want true", got["jira_enabled"])
	}
}

func TestSyncStatus(t *testing.T) {
	srv := setupTestServer(t)
	srv.syncStatus.Track("konflux", time.Minute).Report(nil)
	srv.syncStatus.Track("jira", 0).Report(errors.New("JIRA authentication failed: boom"))

	var got struct {
		Problems []map[string]any `json:"problems"`
	}
	getJSON(t, srv, "/api/v1/sync-status", http.StatusOK, &got)

	if len(got.Problems) != 1 {
		t.Fatalf("problems = %v, want one", got.Problems)
	}
	p := got.Problems[0]
	if p["source"] != "jira" || p["message"] != "JIRA authentication failed: boom" || p["last_success"] != nil {
		t.Errorf("problem = %v", p)
	}
	if _, err := time.Parse(time.RFC3339, p["since"].(string)); err != nil {
		t.Errorf("since = %v, want RFC3339", p["since"])
	}
}

// seedSnapshot creates a snapshot whose components are named comps.
func seedSnapshot(t *testing.T, srv *Server, app, name string, created time.Time, comps ...string) {
	t.Helper()
	id, err := srv.db.CreateSnapshot(t.Context(), app, name, created)
	if err != nil {
		t.Fatalf("create snapshot %s: %v", name, err)
	}
	for _, c := range comps {
		if err := srv.db.CreateSnapshotComponent(t.Context(), id, c, "quay.io/x/"+c+"@"+name); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
	}
}

// seedReleaseView seeds quay, FBC and base-image snapshots for 3.18 plus
// unrelated 3.9 and 3.14 data.
func seedReleaseView(t *testing.T, srv *Server) time.Time {
	t.Helper()
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	hour := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }
	seedSnapshot(t, srv, "quay-3-18", "quay-3-18-a", hour(0), "quay-3-18-quay", "quay-3-18-base-rhel9")
	seedSnapshot(t, srv, "quay-3-18", "quay-3-18-b", hour(0), "quay-3-18-clair")
	seedSnapshot(t, srv, "fbc-quay-3-18", "fbc-quay-3-18-a", hour(1), "fbc-quay-3-18-index")
	seedSnapshot(t, srv, "quay-images-base", "base-a", hour(2), "quay-3-18-base-rhel9", "quay-3-9-base-rhel9")
	seedSnapshot(t, srv, "quay-images-base", "base-b", hour(3), "quay-3-9-base-rhel9")
	seedSnapshot(t, srv, "quay-3-9", "quay-3-9-a", hour(3), "quay-3-9-quay")
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-a", hour(0), "quay-3-14-quay")
	for name, app := range map[string]string{
		"quay-v3.18.0": "quay-3-18",
		"quay-v3.14.0": "quay-3-14",
		"quay-v3.20.0": "quay-3-20",
	} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: name, KonfluxApplication: app}); err != nil {
			t.Fatalf("upsert release %s: %v", name, err)
		}
	}
	return t0
}

func getJSON(t *testing.T, srv *Server, url string, wantStatus int, v any) {
	t.Helper()
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("GET %s: got %d, want %d, body: %s", url, w.Code, wantStatus, w.Body.String())
	}
	if v != nil {
		if err := json.NewDecoder(w.Body).Decode(v); err != nil {
			t.Fatalf("GET %s: decode: %v", url, err)
		}
	}
}

func TestListReleaseSnapshots(t *testing.T) {
	srv := setupTestServer(t)
	t0 := seedReleaseView(t, srv)
	for i, r := range []struct{ name, app, snapshot string }{
		{"fbc-release", "fbc-quay-3-18", "fbc-quay-3-18-a"},
		// Same name, other application: not a Release of quay-3-18-a.
		{"cross-app", "fbc-quay-3-18", "quay-3-18-a"},
		{"stage-gone", "fbc-quay-3-18", "fbc-quay-3-18-gone"},
		{"stage-gone-retry", "fbc-quay-3-18", "fbc-quay-3-18-gone"},
	} {
		if err := srv.db.UpsertKonfluxRelease(t.Context(), &model.KonfluxRelease{
			Name: r.name, Application: r.app, Snapshot: r.snapshot, ReleasedStatus: "Succeeded",
			CreatedAt: t0.Add(time.Duration(4+i) * time.Hour),
		}); err != nil {
			t.Fatalf("upsert release %s: %v", r.name, err)
		}
	}

	type row struct {
		name     string
		count    int
		missing  bool
		releases int
	}
	page := func(url string) ([]row, bool) {
		t.Helper()
		var p model.ReleaseSnapshotPage
		getJSON(t, srv, url, http.StatusOK, &p)
		out := []row{}
		for _, s := range p.Snapshots {
			out = append(out, row{s.Name, s.ComponentCount, s.Missing, len(s.Releases)})
		}
		return out, p.HasMore
	}

	// base-b only carries a 3.9 base image, so 3.18 skips it. The gone
	// Snapshot is dated by its oldest Release.
	all := []row{
		{"fbc-quay-3-18-gone", 0, true, 2},
		{"base-a", 2, false, 0},
		{"fbc-quay-3-18-a", 1, false, 1},
		{"quay-3-18-b", 1, false, 0},
		{"quay-3-18-a", 2, false, 0},
	}
	tests := []struct {
		query   string
		want    []row
		hasMore bool
	}{
		{"", all, false},
		{"?limit=2&offset=1", all[1:3], true},
		{"?limit=2&offset=3", all[3:], false},
		{"?application=quay-images-base", all[1:2], false},
		{"?application=fbc-quay-3-18", []row{all[0], all[2]}, false},
		// quay-3-18-a is only named by a Release of another application.
		{"?with_release=true", []row{all[0], all[2]}, false},
		{"?application=fbc-quay-3-18&with_release=true&offset=1", all[2:3], false},
		{"?application=quay-3-18&with_release=true", []row{}, false},
	}
	for _, tt := range tests {
		got, hasMore := page("/api/v1/releases/quay-v3.18.0/snapshots" + tt.query)
		if !slices.Equal(got, tt.want) || hasMore != tt.hasMore {
			t.Errorf("%q: got %v has_more=%v, want %v has_more=%v", tt.query, got, hasMore, tt.want, tt.hasMore)
		}
	}

	if got, _ := page("/api/v1/releases/quay-v3.20.0/snapshots"); len(got) != 0 {
		t.Errorf("no-data snapshots: got %v, want none", got)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots?application=quay-3-9", http.StatusBadRequest, nil)
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/snapshots", http.StatusNotFound, nil)
}

func TestGetReleaseSnapshot(t *testing.T) {
	srv := setupTestServer(t)
	t0 := seedReleaseView(t, srv)
	if err := srv.db.UpsertKonfluxRelease(t.Context(), &model.KonfluxRelease{
		Name: "fbc-release", Application: "fbc-quay-3-18", Snapshot: "fbc-quay-3-18-a", ReleasedStatus: "Succeeded", CreatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.UpsertKonfluxRelease(t.Context(), &model.KonfluxRelease{
		Name: "fbc-retry", Application: "fbc-quay-3-18", Snapshot: "fbc-quay-3-18-a", ReleasedStatus: "False", ReleasedReason: "Failed",
		FailedTask: "verify-conforma", FailedStep: "assert", CreatedAt: t0.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	var snap model.ReleaseSnapshot
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/fbc-quay-3-18-a", http.StatusOK, &snap)
	wantImages := []model.SnapshotImage{{Name: "fbc-quay-3-18-index", Image: "quay.io/x/fbc-quay-3-18-index@fbc-quay-3-18-a"}}
	if !slices.Equal(snap.Components, wantImages) || len(snap.Releases) != 2 || snap.Releases[1].Name != "fbc-release" {
		t.Fatalf("snapshot: got %+v", snap)
	}
	if newest := snap.Releases[0]; newest.Name != "fbc-retry" || newest.FailedTask != "verify-conforma" || newest.FailedStep != "assert" {
		t.Errorf("newest release: got %+v", newest)
	}

	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/base-a", http.StatusOK, &snap)
	for _, name := range []string{
		"base-b",       // base Snapshot without a 3.18 image
		"quay-3-9-a",   // another release's application
		"no-such-snap", // never stored
	} {
		getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/"+name, http.StatusNotFound, nil)
	}
}

func TestArtBuildLinks(t *testing.T) {
	srv := setupTestServer(t)
	seedReleaseView(t, srv)
	// seedSnapshot pins each image to the digest "@<snapshot name>".
	if err := srv.db.UpsertArtBuild(t.Context(), artbuild.Build{
		Digest: "quay-3-18-b", State: artbuild.StateResolved, NVR: "clair-1", RecordID: "rec-1",
		UpstreamRepo: "https://github.com/quay/clair", UpstreamSHA: "abc123", CheckedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.UpsertArtBuild(t.Context(), artbuild.Build{Digest: "quay-3-18-a", State: artbuild.StateUnresolved, CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	want := &model.ArtBuild{
		NVR:          "clair-1",
		BuildURL:     "https://art.example/build?nvr=clair-1&record_id=rec-1",
		UpstreamRepo: "https://github.com/quay/clair",
		UpstreamSHA:  "abc123",
	}

	var snap model.ReleaseSnapshot
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/quay-3-18-b", http.StatusOK, &snap)
	if len(snap.Components) != 1 || snap.Components[0].Art == nil || *snap.Components[0].Art != *want {
		t.Errorf("snapshot art: got %+v, want %+v", snap.Components, want)
	}

	var unresolved model.ReleaseSnapshot
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/quay-3-18-a", http.StatusOK, &unresolved)
	if len(unresolved.Components) == 0 {
		t.Fatal("unresolved snapshot: no components")
	}
	for _, c := range unresolved.Components {
		if c.Art != nil {
			t.Errorf("%s art: got %+v, want null (unresolved)", c.Name, c.Art)
		}
	}
}

// The Quay Snapshot's bundle is current when the newest quay-operator FBC
// catalog's stable channel names its digest, behind when a fully read channel
// does not, and unknown when the catalog cannot settle it.
func TestFBCCatalogStatus(t *testing.T) {
	const (
		bundleImage = "quay.io/x/art-images@sha256:b24"
		catImage    = "quay.io/x/art-fbc@sha256:fbc"
	)
	inChannel := fbc.Bundle{Package: fbc.Package, Channel: "stable-3.18", Name: "quay-operator.v3.18.1",
		Image: "registry.redhat.io/quay/quay-operator-bundle@sha256:b24", Digest: "sha256:b24"}
	older := fbc.Bundle{Package: fbc.Package, Channel: "stable-3.18", Name: "quay-operator.v3.18.1",
		Image: "registry.redhat.io/quay/quay-operator-bundle@sha256:old", Digest: "sha256:old"}
	tagged := fbc.Bundle{Package: fbc.Package, Channel: "stable-3.18", Name: "quay-operator.v3.18.2",
		Image: "registry.redhat.io/quay/quay-operator-bundle:v3.18.2"}
	prev := fbc.Bundle{Package: fbc.Package, Channel: "stable-3.18", Name: "quay-operator.v3.18.0",
		Image: "registry.redhat.io/quay/quay-operator-bundle@sha256:030", Digest: "sha256:030"}
	otherChannel := inChannel
	otherChannel.Channel = "stable-3.17"
	for _, tc := range []struct {
		name          string
		snapshotImage string
		catalog       *[]fbc.Bundle // nil leaves the catalog unread
		state         string
		want          model.FBCCatalog
	}{
		{"current", bundleImage, &[]fbc.Bundle{prev, inChannel}, fbc.StateParsed,
			model.FBCCatalog{Status: "current", CatalogBundleImage: inChannel.Image}},
		{"behind", bundleImage, &[]fbc.Bundle{older}, fbc.StateParsed,
			model.FBCCatalog{Status: "behind", CatalogBundleImage: older.Image}},
		{"unread catalog", bundleImage, nil, "", model.FBCCatalog{Status: "unknown"}},
		{"failed read", bundleImage, &[]fbc.Bundle{}, fbc.StateFailed, model.FBCCatalog{Status: "unknown"}},
		{"bundle only in another channel", bundleImage, &[]fbc.Bundle{otherChannel}, fbc.StateParsed, model.FBCCatalog{Status: "unknown"}},
		{"tag ref in channel", bundleImage, &[]fbc.Bundle{older, tagged}, fbc.StateParsed,
			model.FBCCatalog{Status: "unknown", CatalogBundleImage: older.Image}},
		{"snapshot bundle by tag", "quay.io/x/art-images:v3.18.1", &[]fbc.Bundle{inChannel}, fbc.StateParsed, model.FBCCatalog{Status: "unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := setupTestServer(t)
			ctx := t.Context()
			t0 := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
			if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
				t.Fatal(err)
			}
			add := func(app, name string, created time.Time, component, image string) {
				t.Helper()
				id, err := srv.db.CreateSnapshot(ctx, app, name, created)
				if err != nil {
					t.Fatal(err)
				}
				if err := srv.db.CreateSnapshotComponent(ctx, id, component, image); err != nil {
					t.Fatal(err)
				}
			}
			add("fbc-quay-3-18", "fbc-3-18-op", t0, "fbc-quay-3-18-quay-operator", catImage)
			// Newer, but not the quay-operator catalog.
			add("fbc-quay-3-18", "fbc-3-18-cso", t0.Add(time.Hour), "fbc-quay-3-18-container-security-operator", "quay.io/x/art-fbc@sha256:c50")
			add("quay-3-18", "quay-3-18-a", t0.Add(2*time.Hour), "quay-3-18-quay-operator-bundle", tc.snapshotImage)
			// Newer, but ART's stage fragment of the quay-operator catalog.
			add("fbc-quay-3-18", "quay-stage-3-18-1-fbc", t0.Add(3*time.Hour), "fbc-quay-3-18-quay-operator", "quay.io/x/art-fbc@sha256:5f0")
			if err := srv.db.UpsertStagedSnapshot(ctx, "quay-stage-3-18-1-fbc", "3.18.1", "fbc", "stage", t0.Add(3*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if tc.catalog != nil {
				if err := srv.db.ReplaceFBCCatalog(ctx, "sha256:fbc", tc.state, *tc.catalog, t0); err != nil {
					t.Fatal(err)
				}
			}

			var snap model.ReleaseSnapshot
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/snapshots/quay-3-18-a", http.StatusOK, &snap)
			want := tc.want
			want.CatalogSnapshot, want.SnapshotBundleImage = "fbc-3-18-op", tc.snapshotImage
			if snap.FBCCatalog == nil || *snap.FBCCatalog != want {
				t.Errorf("fbc_catalog = %+v, want %+v", snap.FBCCatalog, want)
			}
			var fbcSnap model.ReleaseSnapshot
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/snapshots/fbc-3-18-op", http.StatusOK, &fbcSnap)
			if fbcSnap.FBCCatalog != nil {
				t.Errorf("FBC Snapshot fbc_catalog = %+v, want none", fbcSnap.FBCCatalog)
			}
		})
	}
}

// A component row shows a newer ART build in progress only when it started
// after the Snapshot, from another upstream commit, and was seen recently.
func TestPendingArtBuild(t *testing.T) {
	srv := setupTestServer(t)
	t0 := seedReleaseView(t, srv) // quay-3-18-b is created at t0
	const nvr = "quay-clair-container-3.18.1-202610010000.p2.gabc1234.assembly.stream.el9"
	if err := srv.db.UpsertArtBuild(t.Context(), artbuild.Build{
		Digest: "quay-3-18-b", State: artbuild.StateResolved, NVR: nvr, RecordID: "rec-1", UpstreamSHA: "abc123", CheckedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	pending := artbuild.PendingBuild{
		Version: "3.18.1", Name: "quay-clair-container",
		NVR: "quay-clair-container-3.18.1-202610010100.p2.gdef4567.assembly.stream.el9", RecordID: "rec-2",
		UpstreamSHA: "def456", StartedAt: t0.Add(time.Hour),
	}
	want := &model.PendingArtBuild{
		BuildURL:    "https://art.example/build?nvr=quay-clair-container-3.18.1-202610010100.p2.gdef4567.assembly.stream.el9&record_id=rec-2",
		UpstreamSHA: "def456",
		StartedAt:   t0.Add(time.Hour),
	}
	for _, tc := range []struct {
		name    string
		edit    func(p *artbuild.PendingBuild)
		checked time.Duration
		want    *model.PendingArtBuild
	}{
		{"newer build", func(*artbuild.PendingBuild) {}, 0, want},
		{"same upstream commit", func(p *artbuild.PendingBuild) { p.UpstreamSHA = "abc123" }, 0, nil},
		{"started before the snapshot", func(p *artbuild.PendingBuild) { p.StartedAt = t0.Add(-time.Hour) }, 0, nil},
		{"another z-stream", func(p *artbuild.PendingBuild) { p.Version = "3.18.2" }, 0, nil},
		{"stale", func(*artbuild.PendingBuild) {}, -11 * time.Minute, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := pending
			tc.edit(&p)
			if err := srv.db.ReplaceArtPendingBuilds(t.Context(), "quay-3.18", []artbuild.PendingBuild{p}, time.Now().Add(tc.checked)); err != nil {
				t.Fatal(err)
			}
			var snap model.ReleaseSnapshot
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/quay-3-18-b", http.StatusOK, &snap)
			if len(snap.Components) != 1 {
				t.Fatalf("components: got %d, want 1", len(snap.Components))
			}
			got := snap.Components[0].PendingArtBuild
			if (got == nil) != (tc.want == nil) || got != nil && (got.BuildURL != tc.want.BuildURL || got.UpstreamSHA != tc.want.UpstreamSHA || !got.StartedAt.Equal(tc.want.StartedAt)) {
				t.Errorf("pending_art_build: got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Build attempts are the release's z-version only, with a link to each
// record; a stream never searched has no coverage, which reads as unknown.
func TestListBuildAttempts(t *testing.T) {
	srv := setupTestServer(t)
	t0 := seedReleaseView(t, srv)
	const url = "/api/v1/releases/quay-v3.18.0/build-attempts"
	var resp model.BuildAttempts
	getJSON(t, srv, url, http.StatusOK, &resp)
	if resp.CoveredFrom != nil || resp.Attempts == nil || len(resp.Attempts) != 0 {
		t.Errorf("before any search: got %+v, want null coverage and no attempts", resp)
	}

	const nvr = "quay-clair-container-3.18.0-202610010000.p2.gabc1234.assembly.stream.el9"
	attempts := []artbuild.Attempt{
		{Version: "3.18.0", Name: "quay-clair-container", NVR: nvr, RecordID: "rec-1", Outcome: "build_error", StartedAt: t0},
		{Version: "3.18.1", Name: "quay-clair-container", NVR: "quay-clair-container-3.18.1-1.el9", RecordID: "rec-2", Outcome: "success", StartedAt: t0},
	}
	if err := srv.db.StoreArtBuildAttempts(t.Context(), "quay-3.18", attempts, t0.Add(-time.Hour), t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	getJSON(t, srv, url, http.StatusOK, &resp)
	if resp.CoveredFrom == nil || !resp.CoveredFrom.Equal(t0.Add(-time.Hour)) {
		t.Errorf("coverage: got from %v", resp.CoveredFrom)
	}
	want := model.BuildAttempt{
		Component: "quay-clair-container", Outcome: "build_error", StartedAt: t0,
		BuildURL: "https://art.example/build?nvr=" + nvr + "&record_id=rec-1",
	}
	if len(resp.Attempts) != 1 || resp.Attempts[0] != want {
		t.Errorf("attempts:\n got %+v\nwant [%+v]", resp.Attempts, want)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/build-attempts", http.StatusNotFound, nil)
}

func TestGetStaged(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	// Two z-versions of one application each select their own assembly's
	// Snapshots. ART stages no 3.16 version.
	for _, v := range []model.ReleaseVersion{
		{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"},
		{Name: "quay-v3.18.2", KonfluxApplication: "quay-3-18"},
		{Name: "quay-v3.18.3", KonfluxApplication: "quay-3-18"},
		{Name: "quay-v3.16.8", KonfluxApplication: "quay-3-16"},
	} {
		if err := srv.db.UpsertReleaseVersion(ctx, &v); err != nil {
			t.Fatal(err)
		}
	}
	for i, s := range []struct{ name, assembly, kind, component string }{
		{"quay-stage-3-18-1-image", "3.18.1", "image", ""},
		{"quay-stage-3-18-1-fbc", "3.18.1", "fbc", "fbc-quay-3-18-quay-operator"},
		// Staged later, but the other operators' catalogs.
		{"quay-stage-3-18-1-fbc-cso", "3.18.1", "fbc", "fbc-quay-3-18-container-security-operator"},
		{"quay-stage-3-18-1-fbc-qbo", "3.18.1", "fbc", "fbc-quay-3-18-quay-bridge-operator"},
		{"quay-stage-3-18-2-image", "3.18.2", "image", ""},
	} {
		created := t0.Add(time.Duration(i) * time.Minute)
		if err := srv.db.UpsertStagedSnapshot(ctx, s.name, s.assembly, s.kind, "stage", created); err != nil {
			t.Fatal(err)
		}
		if s.component == "" {
			continue
		}
		id, err := srv.db.CreateSnapshot(ctx, "fbc-quay-3-18", s.name, created)
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.db.CreateSnapshotComponent(ctx, id, s.component, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.db.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
		Name: "quay-stage-3-18-1-image", Application: "quay-3-18", Snapshot: "quay-stage-3-18-1-image",
		ReleasedStatus: "False", ReleasedReason: "Failed", FailedTask: "verify-conforma", CreatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}

	var got model.StagedSnapshots
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/staged", http.StatusOK, &got)
	if !got.StreamStaged || got.Image == nil || got.Image.Name != "quay-stage-3-18-1-image" ||
		got.Image.Release == nil || got.Image.Release.ReleasedReason != "Failed" || got.Image.Release.FailedTask != "verify-conforma" {
		t.Errorf("3.18.1: got %+v", got)
	}
	wantCatalogs := []struct{ operator, name string }{
		{"quay-operator", "quay-stage-3-18-1-fbc"},
		{"container-security-operator", "quay-stage-3-18-1-fbc-cso"},
		{"quay-bridge-operator", "quay-stage-3-18-1-fbc-qbo"},
	}
	if len(got.Catalogs) != len(wantCatalogs) {
		t.Fatalf("3.18.1 catalogs = %+v, want %d", got.Catalogs, len(wantCatalogs))
	}
	for i, w := range wantCatalogs {
		if c := got.Catalogs[i]; c.Operator != w.operator || c.Staged == nil || c.Staged.Name != w.name || c.Staged.Release != nil {
			t.Errorf("3.18.1 catalogs[%d] = %s %+v, want %s %s without a Release", i, c.Operator, c.Staged, w.operator, w.name)
		}
	}

	got = model.StagedSnapshots{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/staged", http.StatusOK, &got)
	if !got.StreamStaged || got.Image == nil || got.Image.Name != "quay-stage-3-18-2-image" {
		t.Errorf("3.18.2: got %+v", got)
	}

	// Nothing staged for the version: only the stream tells whether ART stages it.
	for version, stream := range map[string]bool{"quay-v3.18.3": true, "quay-v3.16.8": false} {
		var raw map[string]any
		getJSON(t, srv, "/api/v1/releases/"+version+"/staged", http.StatusOK, &raw)
		want := map[string]any{
			"stream_staged": stream,
			"staged_image":  nil,
			"catalogs": []any{
				map[string]any{"operator": "quay-operator", "staged": nil},
				map[string]any{"operator": "container-security-operator", "staged": nil},
				map[string]any{"operator": "quay-bridge-operator", "staged": nil},
			},
		}
		if !reflect.DeepEqual(raw, want) {
			t.Errorf("%s: got %v, want %v", version, raw, want)
		}
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/staged", http.StatusNotFound, nil)
}

// The ticket list is the release's Target Version tickets plus the .z stream
// tickets a commit of its selected STAGE build names, each listed once.
func TestGetBuildTickets(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	for _, v := range []string{"quay-v3.18.1", "quay-v3.18.2"} {
		if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: v, KonfluxApplication: "quay-3-18"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range []model.JiraIssueRecord{
		{Key: "PROJQUAY-1", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-1", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-2", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-3", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-4", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-5", FixVersion: "quay-v3.18.2"},
	} {
		if err := srv.db.UpsertJiraIssue(ctx, &i); err != nil {
			t.Fatal(err)
		}
	}

	// 3.18.1's STAGE build: quay and clair from GitHub commits, builder unresolved.
	digest := func(c byte) string { return "sha256:" + strings.Repeat(string(c), 64) }
	if err := srv.db.UpsertStagedSnapshot(ctx, "stage-image", "3.18.1", "image", "stage", t0); err != nil {
		t.Fatal(err)
	}
	id, err := srv.db.CreateSnapshot(ctx, "quay-3-18", "stage-image", t0)
	if err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]byte{"quay-3-18-quay-quay": 'a', "quay-3-18-quay-clair": 'c', "quay-3-18-quay-builder": 'b'} {
		if err := srv.db.CreateSnapshotComponent(ctx, id, name, "quay.io/x/"+name+"@"+digest(d)); err != nil {
			t.Fatal(err)
		}
	}
	completed := t0.Add(10 * time.Minute)
	if err := srv.db.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
		Name: "stage-release", Application: "quay-3-18", Snapshot: "stage-image", ReleasePlan: "quay-advisory-stage-3-18",
		ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: t0, CompletionTime: &completed,
	}); err != nil {
		t.Fatal(err)
	}
	quaySHA, clairSHA := strings.Repeat("1", 40), strings.Repeat("2", 40)
	for _, b := range []artbuild.Build{
		{Digest: digest('a'), State: artbuild.StateResolved, UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: quaySHA, CheckedAt: t0},
		{Digest: digest('c'), State: artbuild.StateResolved, UpstreamRepo: "https://github.com/quay/clair", UpstreamSHA: clairSHA, CheckedAt: t0},
	} {
		if err := srv.db.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	// GitHub returns quay's commits and fails clair's compare.
	commit := func(n int) model.BuildCommit {
		sha := fmt.Sprintf("%040x", n)
		return model.BuildCommit{Component: "quay-3-18-quay-quay", CommitSHA: sha, CommitURL: "https://github.com/quay/quay/commit/" + sha}
	}
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/quay/quay/compare/HEAD..."+quaySHA {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var commits []map[string]any
		for i, msg := range []string{"PROJQUAY-1: fix", "PROJQUAY-3: backport"} {
			c := commit(i + 1)
			commits = append(commits, map[string]any{"sha": c.CommitSHA, "html_url": c.CommitURL, "commit": map[string]string{"message": msg}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": len(commits), "commits": commits})
	}))
	defer gh.Close()
	srv.Scanner = github.NewScanner(github.NewClient(gh.URL, "", gh.Client()), srv.db, srv.StageReleasePlanPattern, slog.Default())
	srv.Scanner.ScanOnce(ctx)

	var got model.BuildTickets
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/build-tickets", http.StatusOK, &got)
	if got.Build == nil || got.Build.SnapshotName != "stage-image" || !got.Build.CompletedAt.Equal(completed) || got.Reason != "" {
		t.Errorf("build = %+v, reason %q; want stage-image completed at %v", got.Build, got.Reason, completed)
	}
	wantNotCompared := []model.NotCompared{
		{Component: "quay-3-18-quay-builder", Reason: github.ReasonNoProvenance},
		{Component: "quay-3-18-quay-clair", Reason: github.ReasonAPIError},
	}
	if !slices.Equal(got.NotCompared, wantNotCompared) {
		t.Errorf("not_compared = %+v, want %+v", got.NotCompared, wantNotCompared)
	}
	// PROJQUAY-1 is listed once, as 3.18.1's; PROJQUAY-4 is a .z ticket no
	// commit names.
	want := []struct {
		key, fixVersion string
		inBuild         []model.BuildCommit
	}{
		{"PROJQUAY-1", "quay-v3.18.1", []model.BuildCommit{commit(1)}},
		{"PROJQUAY-2", "quay-v3.18.1", nil},
		{"PROJQUAY-3", "quay-v3.18.z", []model.BuildCommit{commit(2)}},
	}
	if len(got.Tickets) != len(want) {
		t.Fatalf("tickets = %+v, want %d", got.Tickets, len(want))
	}
	for i, w := range want {
		if g := got.Tickets[i]; g.Key != w.key || g.FixVersion != w.fixVersion || !slices.Equal(g.InBuild, w.inBuild) {
			t.Errorf("tickets[%d] = %s %s %+v, want %s %s %+v", i, g.Key, g.FixVersion, g.InBuild, w.key, w.fixVersion, w.inBuild)
		}
	}

	var none struct {
		Build       json.RawMessage `json:"build"`
		Reason      string          `json:"reason"`
		NotCompared json.RawMessage `json:"not_compared"`
		Tickets     []struct {
			Key     string          `json:"key"`
			InBuild json.RawMessage `json:"in_build"`
		} `json:"tickets"`
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/build-tickets", http.StatusOK, &none)
	if string(none.Build) != "null" || none.Reason != "no successful stage release of a staged image snapshot" || string(none.NotCompared) != "[]" ||
		len(none.Tickets) != 1 || none.Tickets[0].Key != "PROJQUAY-5" || string(none.Tickets[0].InBuild) != "[]" {
		t.Errorf("without a build: got %+v", none)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/build-tickets", http.StatusNotFound, nil)

	// Once 3.18.1 is released, the scanner drops its compares and never makes
	// them again.
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true}); err != nil {
		t.Fatal(err)
	}
	srv.Scanner.ScanOnce(ctx)
	var released model.BuildTickets
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/build-tickets", http.StatusOK, &released)
	wantNotCompared = []model.NotCompared{
		{Component: "quay-3-18-quay-builder", Reason: github.ReasonNoProvenance},
		{Component: "quay-3-18-quay-clair", Reason: "version is released or archived"},
		{Component: "quay-3-18-quay-quay", Reason: "version is released or archived"},
	}
	if !slices.Equal(released.NotCompared, wantNotCompared) {
		t.Errorf("released not_compared = %+v, want %+v", released.NotCompared, wantNotCompared)
	}
}

func TestReleasesOverview(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	dueDate := time.Now().Add(10 * 24 * time.Hour)
	err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:               "3.16.3",
		KonfluxApplication: "quay-3-16",
		DueDate:            &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	built := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-snap-1", built, "quay-3-16-quay")

	err = srv.db.UpsertJiraIssue(ctx, &model.JiraIssueRecord{
		Key: "PROJQUAY-1", Summary: "fix bug", Status: "Open",
		Priority: "Major", FixVersion: "3.16.3", IssueType: "Bug",
		Link: "https://redhat.atlassian.net/browse/PROJQUAY-1",
	})
	if err != nil {
		t.Fatalf("upsert issue: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/releases/overview", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("overview: got %d, body: %s", w.Code, w.Body.String())
	}

	if cc := w.Header().Get("Cache-Control"); cc != "max-age=30" {
		t.Errorf("Cache-Control: got %q, want max-age=30", cc)
	}

	var overviews []model.ReleaseOverview
	if err := json.NewDecoder(w.Body).Decode(&overviews); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(overviews) != 1 {
		t.Fatalf("overviews: got %d, want 1", len(overviews))
	}

	ov := overviews[0]
	if ov.Release.Name != "3.16.3" {
		t.Errorf("release name: got %q, want 3.16.3", ov.Release.Name)
	}
	if ov.IssueSummary == nil {
		t.Fatal("issue_summary: got nil")
	}
	if ov.IssueSummary.Total != 1 {
		t.Errorf("issue_summary: got total=%d, want 1", ov.IssueSummary.Total)
	}
	if ov.LatestBuild == nil || !ov.LatestBuild.Equal(built) {
		t.Errorf("latest_build: got %v, want %v", ov.LatestBuild, built)
	}
	if ov.Readiness.Signal != "yellow" {
		t.Errorf("readiness: got %q, want yellow (open issues remain)", ov.Readiness.Signal)
	}
}

func TestGetIssueSummariesBatch(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	issues := []model.JiraIssueRecord{
		{Key: "Q-1", Summary: "bug1", Status: "Open", Priority: "Major", FixVersion: "3.16.3", IssueType: "Bug"},
		{Key: "Q-2", Summary: "cve1", Status: "Closed", Priority: "Critical", FixVersion: "3.16.3", IssueType: "Vulnerability"},
		{Key: "Q-3", Summary: "task1", Status: "Verified", Priority: "Minor", FixVersion: "3.17.0", IssueType: "Story"},
		{Key: "Q-4", Summary: "bug2", Status: "Release Pending", Priority: "Major", FixVersion: "3.17.0", IssueType: "Bug"},
	}
	for _, issue := range issues {
		if err := srv.db.UpsertJiraIssue(ctx, &issue); err != nil {
			t.Fatalf("upsert issue %s: %v", issue.Key, err)
		}
	}

	summaries, err := srv.db.GetIssueSummariesBatch(ctx, []string{"3.16.3", "3.17.0", "nonexistent"})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}

	s163 := summaries["3.16.3"]
	if s163 == nil {
		t.Fatal("3.16.3 summary: got nil")
	}
	if s163.Total != 2 {
		t.Errorf("3.16.3 total: got %d, want 2", s163.Total)
	}
	if s163.CVEs != 1 {
		t.Errorf("3.16.3 cves: got %d, want 1", s163.CVEs)
	}

	s170 := summaries["3.17.0"]
	if s170 == nil {
		t.Fatal("3.17.0 summary: got nil")
	}
	// Release Pending counts as verified, in the batch and for one version.
	want := model.IssueSummary{Total: 2, Verified: 2}
	if *s170 != want {
		t.Errorf("3.17.0: got %+v, want %+v", *s170, want)
	}
	one, err := srv.db.GetIssueSummary(ctx, "3.17.0")
	if err != nil {
		t.Fatal(err)
	}
	if *one != want {
		t.Errorf("GetIssueSummary 3.17.0: got %+v, want %+v", *one, want)
	}

	if summaries["nonexistent"] != nil {
		t.Errorf("nonexistent: got %+v, want nil", summaries["nonexistent"])
	}
}

func TestGetReleaseReadiness(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	// Create a release with a future due date
	dueDate := time.Now().Add(10 * 24 * time.Hour)
	err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:               "3.16.3",
		KonfluxApplication: "quay-3-16",
		DueDate:            &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	getReadiness := func() model.ReadinessResponse {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/releases/3.16.3/readiness", nil)
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get readiness: got %d, body: %s", w.Code, w.Body.String())
		}
		var readiness model.ReadinessResponse
		if err := json.NewDecoder(w.Body).Decode(&readiness); err != nil {
			t.Fatal(err)
		}
		return readiness
	}

	if got := getReadiness(); got.Signal != "yellow" || got.Message != "No build snapshots yet" {
		t.Errorf("without snapshot: got %+v, want yellow/No build snapshots yet", got)
	}

	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-snap-1", time.Now(), "quay-3-16-quay")

	if got := getReadiness(); got.Signal != "green" || got.Message != "No open issues" {
		t.Errorf("with snapshot: got %+v, want green/No open issues", got)
	}
}

func TestComputeReadiness(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	open := &model.IssueSummary{Total: 2, Open: 1}
	for _, tc := range []struct {
		desc           string
		released       bool
		catalogShipped bool
		want           model.ReadinessResponse
	}{
		{"released in JIRA", true, false, model.ReadinessResponse{Signal: "green", Message: "Released", Shipped: true}},
		{"catalog-shipped, JIRA unreleased", false, true, model.ReadinessResponse{Signal: "green", Message: "Shipped", Shipped: true}},
		{"unshipped past due", false, false, model.ReadinessResponse{Signal: "red", Message: "Past due date"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			release := &model.ReleaseVersion{Name: "quay-v3.17.5", Released: tc.released, DueDate: &past}
			if got := computeReadiness(release, open, true, tc.catalogShipped); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMarkNextInStream(t *testing.T) {
	type row struct {
		name    string
		shipped bool
	}
	for _, tc := range []struct {
		desc string
		rows []row
		want []string
	}{
		{"lowest z wins, double-digit z", []row{{"quay-v3.15.10", false}, {"quay-v3.15.9", false}}, []string{"quay-v3.15.9"}},
		{"shipped z is skipped", []row{{"quay-v3.17.5", true}, {"quay-v3.17.7", false}, {"quay-v3.17.6", false}}, []string{"quay-v3.17.6"}},
		{"all shipped shows nothing", []row{{"quay-v3.9.27", true}}, nil},
		{"OMR streams by major.minor", []row{{"omr-v2.0.13", false}, {"omr-v3.0.0", false}, {"omr-v3.0.1", false}}, []string{"omr-v2.0.13", "omr-v3.0.0"}},
		{"products do not share a stream", []row{{"quay-v3.0.2", false}, {"omr-v3.0.1", false}}, []string{"quay-v3.0.2", "omr-v3.0.1"}},
		{"unparsed name is its own stream", []row{{"3.16.3", false}, {"3.16.4", false}}, []string{"3.16.3", "3.16.4"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			overviews := make([]model.ReleaseOverview, len(tc.rows))
			for i, r := range tc.rows {
				overviews[i] = model.ReleaseOverview{Release: model.ReleaseVersion{Name: r.name}, Shipped: r.shipped}
			}
			markNextInStream(overviews)
			var got []string
			for _, ov := range overviews {
				if ov.NextInStream {
					got = append(got, ov.Release.Name)
				}
			}
			slices.Sort(got)
			slices.Sort(tc.want)
			if !slices.Equal(got, tc.want) {
				t.Errorf("next in stream: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReleasesOverviewShipped(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":1,"data":[{"repositories":[{"published":true,"tags":[{"name":"v3.17.5"}]}]}]}`))
	}))
	defer cat.Close()
	srv.shipped = catalog.NewShipped(catalog.NewClient(cat.URL, cat.Client()), slog.Default())
	srv.shipped.Refresh(ctx)

	for _, rv := range []model.ReleaseVersion{
		{Name: "quay-v3.17.5", KonfluxApplication: "quay-3-17"},
		{Name: "quay-v3.17.6", KonfluxApplication: "quay-3-17"},
		{Name: "quay-v3.18.0", KonfluxApplication: "quay-3-18", Released: true},
		{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"},
	} {
		if err := srv.db.UpsertReleaseVersion(ctx, &rv); err != nil {
			t.Fatalf("upsert release: %v", err)
		}
	}

	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/releases/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("overview: got %d, body: %s", w.Code, w.Body.String())
	}
	var overviews []model.ReleaseOverview
	if err := json.NewDecoder(w.Body).Decode(&overviews); err != nil {
		t.Fatalf("decode: %v", err)
	}
	type flags struct{ shipped, next bool }
	want := map[string]flags{
		"quay-v3.17.5": {true, false},
		"quay-v3.17.6": {false, true},
		"quay-v3.18.0": {true, false},
		"quay-v3.18.1": {false, true},
	}
	for _, ov := range overviews {
		got := flags{ov.Shipped, ov.NextInStream}
		if got != want[ov.Release.Name] {
			t.Errorf("%s: got %+v, want %+v", ov.Release.Name, got, want[ov.Release.Name])
		}
	}
	if len(overviews) != len(want) {
		t.Errorf("overviews: got %d, want %d", len(overviews), len(want))
	}
}
