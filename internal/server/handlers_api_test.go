package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/model"
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
	return New(database, ":0", "https://redhat.atlassian.net", "PROJQUAY", slog.Default())
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

func TestListSnapshots(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	_, err := srv.db.CreateSnapshot(ctx, "quay-3-17", "quay-3-17-20260213-000", time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/snapshots", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list snapshots: got %d, body: %s", w.Code, w.Body.String())
	}

	var snapshots []model.SnapshotRecord
	if err := json.NewDecoder(w.Body).Decode(&snapshots); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Errorf("snapshots: got %d, want 1", len(snapshots))
	}
	if snapshots[0].Application != "quay-3-17" {
		t.Errorf("application: got %q, want %q", snapshots[0].Application, "quay-3-17")
	}
}

// seedSnapshot creates a snapshot whose components are named comps.
func seedSnapshot(t *testing.T, srv *Server, app, name string, created time.Time, comps ...string) {
	t.Helper()
	snap, err := srv.db.CreateSnapshot(t.Context(), app, name, created)
	if err != nil {
		t.Fatalf("create snapshot %s: %v", name, err)
	}
	for _, c := range comps {
		if err := srv.db.CreateSnapshotComponent(t.Context(), snap.ID, c, "sha-"+name, "quay.io/x/"+c+"@"+name, ""); err != nil {
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

func TestGetReleaseComponents(t *testing.T) {
	srv := setupTestServer(t)
	t0 := seedReleaseView(t, srv)

	type comp struct{ name, app, snapshot string }
	tests := []struct {
		release string
		want    []comp
		asOf    time.Time
	}{
		{"quay-v3.18.0", []comp{
			{"fbc-quay-3-18-index", "fbc-quay-3-18", "fbc-quay-3-18-a"},
			{"quay-3-18-base-rhel9", "quay-images-base", "base-a"},
			{"quay-3-18-clair", "quay-3-18", "quay-3-18-b"},
			{"quay-3-18-quay", "quay-3-18", "quay-3-18-a"},
		}, t0.Add(2 * time.Hour)},
		// No FBC for 3.14: falls back to the quay app.
		{"quay-v3.14.0", []comp{{"quay-3-14-quay", "quay-3-14", "quay-3-14-a"}}, t0},
		{"quay-v3.20.0", []comp{}, time.Time{}},
	}
	for _, tt := range tests {
		var got model.ReleaseComponents
		getJSON(t, srv, "/api/v1/releases/"+tt.release+"/components", http.StatusOK, &got)
		gotComps := []comp{}
		for _, c := range got.Components {
			gotComps = append(gotComps, comp{c.Name, c.Application, c.Snapshot})
		}
		if got.Release != tt.release || !slices.Equal(gotComps, tt.want) {
			t.Errorf("%s: got %s %v, want %v", tt.release, got.Release, gotComps, tt.want)
		}
		if got.Components == nil {
			t.Errorf("%s: components is null, want []", tt.release)
		}
		var asOf time.Time
		if got.AsOf != nil {
			asOf = *got.AsOf
		}
		if !asOf.Equal(tt.asOf) {
			t.Errorf("%s: as_of = %v, want %v", tt.release, asOf, tt.asOf)
		}
	}

	var readiness model.ReadinessResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.20.0/readiness", http.StatusOK, &readiness)
	if readiness.Message != "No build snapshots yet" {
		t.Errorf("no-data readiness: got %+v, want No build snapshots yet", readiness)
	}

	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/components", http.StatusNotFound, nil)
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

	var snap model.ReleaseSnapshot
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/snapshots/fbc-quay-3-18-a", http.StatusOK, &snap)
	wantImages := []model.SnapshotImage{{Name: "fbc-quay-3-18-index", Image: "quay.io/x/fbc-quay-3-18-index@fbc-quay-3-18-a", GitSHA: "sha-fbc-quay-3-18-a"}}
	if !slices.Equal(snap.Components, wantImages) || len(snap.Releases) != 1 || snap.Releases[0].Name != "fbc-release" {
		t.Errorf("snapshot: got %+v", snap)
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
		Link: "https://redhat.atlassian.net/browse/PROJQUAY-1", UpdatedAt: time.Now(),
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
	if ov.IssueSummary.Total != 1 || ov.IssueSummary.Bugs != 1 {
		t.Errorf("issue_summary: got total=%d bugs=%d, want 1/1", ov.IssueSummary.Total, ov.IssueSummary.Bugs)
	}
	if ov.ComponentCount != 1 || ov.LatestBuild == nil || !ov.LatestBuild.Equal(built) {
		t.Errorf("components: got count=%d latest_build=%v, want 1/%v", ov.ComponentCount, ov.LatestBuild, built)
	}
	if ov.Readiness.Signal != "yellow" {
		t.Errorf("readiness: got %q, want yellow (open issues remain)", ov.Readiness.Signal)
	}
}

func TestGetIssueSummariesBatch(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	issues := []model.JiraIssueRecord{
		{Key: "Q-1", Summary: "bug1", Status: "Open", Priority: "Major", FixVersion: "3.16.3", IssueType: "Bug", UpdatedAt: time.Now()},
		{Key: "Q-2", Summary: "cve1", Status: "Closed", Priority: "Critical", FixVersion: "3.16.3", IssueType: "Vulnerability", UpdatedAt: time.Now()},
		{Key: "Q-3", Summary: "task1", Status: "Verified", Priority: "Minor", FixVersion: "3.17.0", IssueType: "Story", UpdatedAt: time.Now()},
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
	if s163.Bugs != 1 {
		t.Errorf("3.16.3 bugs: got %d, want 1", s163.Bugs)
	}
	if s163.CVEs != 1 {
		t.Errorf("3.16.3 cves: got %d, want 1", s163.CVEs)
	}

	s170 := summaries["3.17.0"]
	if s170 == nil {
		t.Fatal("3.17.0 summary: got nil")
	}
	if s170.Total != 1 || s170.Verified != 1 {
		t.Errorf("3.17.0: got total=%d verified=%d, want 1/1", s170.Total, s170.Verified)
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

func TestListKonfluxReleases(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, r := range []struct{ name, app string }{
		{"fbc-r1", "fbc-quay-3-18"},
		{"quay-r1", "quay-3-18"},
		{"fbc-r2", "fbc-quay-3-18"},
		{"fbc-r3", "fbc-quay-3-18"},
	} {
		err := srv.db.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: r.name, Application: r.app, CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("upsert release: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/v1/konflux-releases?application=fbc-quay-3-18&limit=2", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list konflux releases: got %d, body: %s", w.Code, w.Body.String())
	}
	var releases []model.KonfluxRelease
	if err := json.NewDecoder(w.Body).Decode(&releases); err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 || releases[0].Name != "fbc-r3" || releases[1].Name != "fbc-r2" {
		t.Errorf("releases: got %+v, want fbc-r3, fbc-r2", releases)
	}
}
