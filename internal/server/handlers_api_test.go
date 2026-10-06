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
	return New(database, nil, ":0", "https://redhat.atlassian.net", "PROJQUAY", slog.Default())
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

	_, err := srv.db.CreateSnapshot(ctx, "quay-v3-17", "quay-v3-17-20260213-000", true, time.Now())
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

	// Create a snapshot for the S3 application
	_, err := srv.db.CreateSnapshot(ctx, "quay-v3-16", "quay-v3-16-snap-1", true, time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	// Create the release version pointing to this S3 app
	err = srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:          "3.16.3",
		S3Application: "quay-v3-16",
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
	if snap.Name != "quay-v3-16-snap-1" {
		t.Errorf("snapshot name: got %q, want quay-v3-16-snap-1", snap.Name)
	}
}

func TestReleasesOverview(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	dueDate := time.Now().Add(10 * 24 * time.Hour)
	err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:          "3.16.3",
		S3Application: "quay-v3-16",
		DueDate:       &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	_, err = srv.db.CreateSnapshot(ctx, "quay-v3-16", "quay-v3-16-snap-1", true, time.Now())
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
	if ov.Snapshot.Name != "quay-v3-16-snap-1" {
		t.Errorf("snapshot name: got %q, want quay-v3-16-snap-1", ov.Snapshot.Name)
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
		Name:          "3.16.3",
		S3Application: "quay-v3-16",
		DueDate:       &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	// Create a passing snapshot
	_, err = srv.db.CreateSnapshot(ctx, "quay-v3-16", "quay-v3-16-snap-1", true, time.Now())
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

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
	if readiness.Signal != "green" {
		t.Errorf("signal: got %q, want green", readiness.Signal)
	}
}

func TestReleaseApplications(t *testing.T) {
	tests := []struct {
		s3App string
		want  []string
	}{
		{"", nil},
		{"quay-v3-16", []string{"quay-v3-16", "quay-3-16", "fbc-quay-3-16"}},
		{"omr-v2-0", []string{"omr-v2-0", "omr-2-0", "fbc-omr-2-0"}},
		{"foo-vault-v1-2", []string{"foo-vault-v1-2", "foo-vault-1-2", "fbc-foo-vault-1-2"}},
		{"quay", []string{"quay"}},
		{"-v3-16", []string{"-v3-16"}},
		{"quay-v", []string{"quay-v"}},
	}
	for _, tt := range tests {
		if got := releaseApplications(tt.s3App); !slices.Equal(got, tt.want) {
			t.Errorf("releaseApplications(%q) = %q, want %q", tt.s3App, got, tt.want)
		}
	}
}

func TestReleaseJoinsKonfluxApplications(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	dueDate := time.Now().Add(10 * 24 * time.Hour)
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:          "3.16.3",
		S3Application: "quay-v3-16",
		DueDate:       &dueDate,
	}); err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	now := time.Now()
	for _, s := range []struct {
		app, name string
		passed    bool
		age       time.Duration
	}{
		{"quay-v3-16", "s3-snap", true, 3 * time.Hour},
		{"quay-3-16", "konflux-snap", true, 2 * time.Hour},
		// Newest, but fbc must not be picked as the release snapshot.
		{"fbc-quay-3-16", "fbc-snap", false, time.Hour},
		{"quay-3-17", "other-release-snap", true, 0},
	} {
		snap, err := srv.db.CreateSnapshot(ctx, s.app, s.name, s.passed, now.Add(-s.age))
		if err != nil {
			t.Fatalf("create snapshot %s: %v", s.name, err)
		}
		status := "passed"
		if !s.passed {
			status = "failed"
		}
		if _, err := srv.db.CreateTestSuite(ctx, snap.ID, "e2e", status, "", "", "", 1, 0, 0, 0, 0, 0, 0, 0, 0, 0); err != nil {
			t.Fatalf("create test suite %s: %v", s.name, err)
		}
	}

	get := func(path string, v any) {
		t.Helper()
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d, body: %s", path, w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(v); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
	}

	var snap model.SnapshotRecord
	get("/api/v1/releases/3.16.3/snapshot", &snap)
	if snap.Name != "konflux-snap" {
		t.Errorf("release snapshot: got %q, want konflux-snap", snap.Name)
	}

	// The fbc snapshot's failing tests count toward readiness.
	var readiness model.ReadinessResponse
	get("/api/v1/releases/3.16.3/readiness", &readiness)
	if readiness.Message != "Integration tests failing" {
		t.Errorf("readiness: got %q, want Integration tests failing", readiness.Message)
	}

	var overviews []model.ReleaseOverview
	get("/api/v1/releases/overview", &overviews)
	if len(overviews) != 1 || overviews[0].Snapshot == nil {
		t.Fatalf("overview: got %+v, want one release with a snapshot", overviews)
	}
	if overviews[0].Snapshot.Name != "konflux-snap" {
		t.Errorf("overview snapshot: got %q, want konflux-snap", overviews[0].Snapshot.Name)
	}
	if overviews[0].Readiness != readiness {
		t.Errorf("overview readiness: got %+v, want %+v", overviews[0].Readiness, readiness)
	}

	var list []model.SnapshotRecord
	get("/api/v1/snapshots?release=3.16.3", &list)
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
	}
	if want := []string{"fbc-snap", "konflux-snap", "s3-snap"}; !slices.Equal(names, want) {
		t.Errorf("snapshots for release: got %q, want %q", names, want)
	}

	// An empty application filter lists everything, as before ?release= existed.
	list = nil
	get("/api/v1/snapshots?application=", &list)
	if len(list) != 4 {
		t.Errorf("snapshots with empty application: got %d, want 4", len(list))
	}

	// Without the early return, an empty S3 application would list every snapshot.
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "3.99.0"}); err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	list = nil
	get("/api/v1/snapshots?release=3.99.0", &list)
	if list == nil || len(list) != 0 {
		t.Errorf("snapshots for release without application: got %+v, want []", list)
	}

	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/snapshots?release=9.9.9", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown release: got %d, want 404", w.Code)
	}
}
