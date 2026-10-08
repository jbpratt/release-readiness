package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	_, err := srv.db.CreateSnapshot(ctx, "quay-v3-17", "quay-v3-17-20260213-000", time.Now())
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
	if snapshots[0].Application != "quay-v3-17" {
		t.Errorf("application: got %q, want %q", snapshots[0].Application, "quay-v3-17")
	}
}

func TestGetReleaseSnapshot(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	// Create a snapshot for the Konflux application
	_, err := srv.db.CreateSnapshot(ctx, "quay-3-16", "quay-3-16-snap-1", time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	// Create the release version pointing to this Konflux app
	err = srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:               "3.16.3",
		KonfluxApplication: "quay-3-16",
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/releases/3.16.3/snapshot", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get release snapshot: got %d, body: %s", w.Code, w.Body.String())
	}

	var snap model.SnapshotRecord
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.Name != "quay-3-16-snap-1" {
		t.Errorf("snapshot name: got %q, want quay-3-16-snap-1", snap.Name)
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

	_, err = srv.db.CreateSnapshot(ctx, "quay-3-16", "quay-3-16-snap-1", time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

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
	if ov.Snapshot == nil {
		t.Fatal("snapshot: got nil")
	}
	if ov.Snapshot.Name != "quay-3-16-snap-1" {
		t.Errorf("snapshot name: got %q, want quay-3-16-snap-1", ov.Snapshot.Name)
	}
	// Snapshot should not include components or test_results in overview
	if ov.Snapshot.Components != nil {
		t.Errorf("snapshot components: should be nil in overview, got %d", len(ov.Snapshot.Components))
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

	_, err = srv.db.CreateSnapshot(ctx, "quay-3-16", "quay-3-16-snap-1", time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

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
