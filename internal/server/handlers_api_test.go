package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
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
	var resp map[string]string
	getJSON(t, setupTestServer(t), "/api/v1/health", http.StatusOK, &resp)
	if resp["status"] != "healthy" {
		t.Errorf("status: got %q, want healthy", resp["status"])
	}
}

func TestConfig(t *testing.T) {
	srv := setupTestServer(t)
	var got map[string]any
	getJSON(t, srv, "/api/v1/config", http.StatusOK, &got)
	if got["jira_enabled"] != false {
		t.Errorf("without a jira sync: jira_enabled = %v, want false", got["jira_enabled"])
	}
	if got["konflux_ui_url"] != "" || got["konflux_namespace"] != "" {
		t.Errorf("unset: konflux_ui_url = %v, konflux_namespace = %v, want both empty", got["konflux_ui_url"], got["konflux_namespace"])
	}

	srv.syncStatus.Track("jira", time.Minute)
	srv.KonfluxUIURL, srv.KonfluxNamespace = "https://konflux.example", "art-quay-tenant"
	getJSON(t, srv, "/api/v1/config", http.StatusOK, &got)
	if got["jira_enabled"] != true {
		t.Errorf("with a jira sync: jira_enabled = %v, want true", got["jira_enabled"])
	}
	if got["konflux_ui_url"] != "https://konflux.example" || got["konflux_namespace"] != "art-quay-tenant" {
		t.Errorf("set: konflux_ui_url = %v, konflux_namespace = %v", got["konflux_ui_url"], got["konflux_namespace"])
	}
}

func TestSyncStatus(t *testing.T) {
	srv := setupTestServer(t)
	srv.syncStatus.Track("konflux", time.Minute).Report(nil)
	srv.syncStatus.Track("jira", 0).Report(errors.New("JIRA authentication failed: boom"))

	var got struct {
		Problems []map[string]any `json:"problems"`
		Sources  []map[string]any `json:"sources"`
	}
	getJSON(t, srv, "/api/v1/sync-status", http.StatusOK, &got)

	if len(got.Sources) != 2 || got.Sources[0]["source"] != "jira" || got.Sources[0]["last_success"] != nil ||
		got.Sources[1]["source"] != "konflux" || got.Sources[1]["last_success"] == nil {
		t.Errorf("sources = %v, want jira never then konflux", got.Sources)
	}

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

func TestSyncStatusSeeded(t *testing.T) {
	srv := setupTestServer(t)
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if err := srv.db.UpsertSyncStatus(t.Context(), "konflux", t0); err != nil {
		t.Fatal(err)
	}
	seed, err := srv.db.SyncStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	srv.syncStatus.Persist(seed, nil)
	srv.syncStatus.Track("konflux", time.Minute)

	var got struct {
		Sources []struct {
			Source      string     `json:"source"`
			LastSuccess *time.Time `json:"last_success"`
		} `json:"sources"`
	}
	getJSON(t, srv, "/api/v1/sync-status", http.StatusOK, &got)
	if len(got.Sources) != 1 || got.Sources[0].LastSuccess == nil || !got.Sources[0].LastSuccess.Equal(t0) {
		t.Errorf("sources = %+v, want konflux last success %v", got.Sources, t0)
	}
}

// seedSnapshot creates a snapshot whose components are named comps, each
// image labelled <name>/<component>.
func seedSnapshot(t *testing.T, srv *Server, app, name string, created time.Time, comps ...string) {
	t.Helper()
	id, err := srv.db.CreateSnapshot(t.Context(), app, name, created)
	if err != nil {
		t.Fatalf("create snapshot %s: %v", name, err)
	}
	for _, c := range comps {
		if err := srv.db.CreateSnapshotComponent(t.Context(), id, c, testImage(name+"/"+c)); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
	}
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

// A version's issues are its Target Version tickets, by key, and [] when it
// has none.
func TestListReleaseIssues(t *testing.T) {
	srv := setupTestServer(t)
	for _, i := range []model.JiraIssueRecord{
		{Key: "PROJQUAY-2", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-3", FixVersion: "quay-v3.18.2"},
		{Key: "PROJQUAY-1", FixVersion: "quay-v3.18.1", Status: "Release Pending"},
	} {
		if err := srv.db.UpsertJiraIssue(t.Context(), &i); err != nil {
			t.Fatal(err)
		}
	}

	var got []model.JiraIssueRecord
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/issues", http.StatusOK, &got)
	if len(got) != 2 || got[0].Key != "PROJQUAY-1" || got[1].Key != "PROJQUAY-2" {
		t.Errorf("3.18.1 issues = %+v, want PROJQUAY-1, PROJQUAY-2", got)
	}
	if got[0].Status != "Release Pending" {
		t.Errorf("PROJQUAY-1 status = %q, want Release Pending", got[0].Status)
	}

	var none json.RawMessage
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.3/issues", http.StatusOK, &none)
	if string(none) != "[]" {
		t.Errorf("3.18.3 issues = %s, want []", none)
	}
}

func TestReleasesOverview(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	dueDate := time.Now().Add(10 * 24 * time.Hour)
	err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:               "quay-v3.16.3",
		KonfluxApplication: "quay-3-16",
		DueDate:            &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	built := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-snap-1", built, "quay-3-16-quay")
	resolveNVR(t, srv, "quay-3-16-snap-1/quay-3-16-quay", testNVR("quay-quay", "3.16.3"))

	err = srv.db.UpsertJiraIssue(ctx, &model.JiraIssueRecord{
		Key: "PROJQUAY-1", Summary: "fix bug",
		FixVersion: "quay-v3.16.3", IssueType: "Bug",
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
	if ov.Release.Name != "quay-v3.16.3" {
		t.Errorf("release name: got %q, want quay-v3.16.3", ov.Release.Name)
	}
	if ov.IssueSummary == nil {
		t.Fatal("issue_summary: got nil")
	}
	if ov.IssueSummary.CVEs != 0 {
		t.Errorf("issue_summary: got cves=%d, want 0", ov.IssueSummary.CVEs)
	}
	if ov.LatestBuild == nil || !ov.LatestBuild.Equal(built) {
		t.Errorf("latest_build: got %v, want %v", ov.LatestBuild, built)
	}
	if ov.Readiness != (model.ReadinessResponse{}) {
		t.Errorf("readiness: got %+v, want none for an open issue", ov.Readiness)
	}
}

func TestGetIssueSummariesBatch(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	issues := []model.JiraIssueRecord{
		{Key: "Q-1", Summary: "bug1", FixVersion: "3.16.3", IssueType: "Bug"},
		{Key: "Q-2", Summary: "cve1", FixVersion: "3.16.3", IssueType: "Vulnerability"},
		{Key: "Q-3", Summary: "task1", FixVersion: "3.17.0", IssueType: "Story"},
		{Key: "Q-4", Summary: "bug2", FixVersion: "3.17.0", IssueType: "Bug"},
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
	if s163.CVEs != 1 {
		t.Errorf("3.16.3 cves: got %d, want 1", s163.CVEs)
	}

	s170 := summaries["3.17.0"]
	if s170 == nil {
		t.Fatal("3.17.0 summary: got nil")
	}
	if s170.CVEs != 0 {
		t.Errorf("3.17.0 cves: got %d, want 0", s170.CVEs)
	}

	if summaries["nonexistent"] != nil {
		t.Errorf("nonexistent: got %+v, want nil", summaries["nonexistent"])
	}
}

func TestReleasesOverviewReadiness(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()

	// Create a release with a future due date
	dueDate := time.Now().Add(10 * 24 * time.Hour)
	err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{
		Name:               "quay-v3.16.3",
		KonfluxApplication: "quay-3-16",
		DueDate:            &dueDate,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	getReadiness := func() model.ReadinessResponse {
		t.Helper()
		var overviews []model.ReleaseOverview
		getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &overviews)
		if len(overviews) != 1 {
			t.Fatalf("overviews: got %d, want 1", len(overviews))
		}
		return overviews[0].Readiness
	}

	if got := getReadiness(); got.Signal != "yellow" || got.Message != "No image snapshot yet" {
		t.Errorf("without snapshot: got %+v, want yellow/No image snapshot yet", got)
	}

	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-snap-1", time.Now(), "quay-3-16-quay")
	resolveNVR(t, srv, "quay-3-16-snap-1/quay-3-16-quay", testNVR("quay-quay", "3.16.3"))

	if got := getReadiness(); got != (model.ReadinessResponse{}) {
		t.Errorf("with snapshot: got %+v, want none", got)
	}
}

// Releases come only from JIRA fixVersions: a quay version ART staged
// without one is not listed and has no page.
func TestReleasesOverviewStagedWithoutJira(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"3.18.1", "5.0.0"} {
		if err := srv.db.UpsertStagedSnapshot(ctx, "quay-stage-"+a, a, "image", "stage"); err != nil {
			t.Fatal(err)
		}
	}

	var overviews []model.ReleaseOverview
	getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &overviews)
	var got []string
	for _, ov := range overviews {
		got = append(got, ov.Release.Name)
	}
	if want := []string{"quay-v3.18.1"}; !slices.Equal(got, want) {
		t.Errorf("overview releases = %v, want %v", got, want)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v5.0.0", http.StatusNotFound, nil)
	getJSON(t, srv, "/api/v1/releases/quay-v5.0.0/candidate", http.StatusNotFound, nil)
}

// omr-v2.0.13 is not a version ART builds, so it has no build to wait on.
func TestReleasesOverviewReadinessUntracked(t *testing.T) {
	srv := setupTestServer(t)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "omr-v2.0.13", KonfluxApplication: "omr-2-0"}); err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	var overviews []model.ReleaseOverview
	getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &overviews)
	want := model.ReadinessResponse{}
	if len(overviews) != 1 || overviews[0].Readiness != want {
		t.Errorf("overviews = %+v, want omr-v2.0.13 readiness %+v", overviews, want)
	}
}

// A version's latest build is its newest build or its STAGE build, whichever
// is newer, shipped or not; one with neither has none.
func TestReleasesOverviewLatestBuild(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	overviews := func() map[string]model.ReleaseOverview {
		t.Helper()
		var list []model.ReleaseOverview
		getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &list)
		byName := map[string]model.ReleaseOverview{}
		for _, ov := range list {
			byName[ov.Release.Name] = ov
		}
		return byName
	}

	// 3.18.1's newest build is newer than its STAGE build; 3.18.2 has neither.
	got := overviews()
	if lb := got["quay-v3.18.1"].LatestBuild; lb == nil || !lb.Equal(mustTime(t, "2026-10-10T01:48:34Z")) {
		t.Errorf("3.18.1 latest_build = %v, want its newest build's", lb)
	}
	if ov := got["quay-v3.18.2"]; ov.LatestBuild != nil || ov.Readiness.Message != "No image snapshot yet" {
		t.Errorf("3.18.2 latest_build = %v, readiness %+v; want none", ov.LatestBuild, ov.Readiness)
	}

	// Once shipped, 3.18.1 keeps it; its newer assembly's stage Release
	// succeeds.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true}); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261010020000", "quay-stage-3-18-1-image-20261010020000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T02:00:00Z", "2026-10-10T02:05:00Z")
	if ov := overviews()["quay-v3.18.1"]; ov.LatestBuild == nil || !ov.LatestBuild.Equal(mustTime(t, "2026-10-10T02:00:00Z")) {
		t.Errorf("shipped 3.18.1 latest_build = %v, want its STAGE build's", ov.LatestBuild)
	}
}

func TestComputeReadiness(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	for _, tc := range []struct {
		desc           string
		released       bool
		catalogShipped bool
		want           model.ReadinessResponse
	}{
		{"released in JIRA", true, false, model.ReadinessResponse{Signal: "green", Message: "Released"}},
		{"catalog-shipped, JIRA unreleased", false, true, model.ReadinessResponse{Signal: "green", Message: "Shipped"}},
		{"unshipped past due", false, false, model.ReadinessResponse{Signal: "red", Message: "Past due date"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			release := &model.ReleaseVersion{Name: "quay-v3.17.5", Released: tc.released, DueDate: &past}
			if got := computeReadiness(release, true, tc.catalogShipped, time.Now()); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Due dates count in whole UTC days, as the version page counts them: at
// 23:59Z a version due that day is due today, not past due.
func TestComputeReadinessDue(t *testing.T) {
	now := mustTime(t, "2026-10-10T23:59:00Z")
	for _, tc := range []struct {
		desc string
		due  string
		want model.ReadinessResponse
	}{
		{"due yesterday", "2026-10-09", model.ReadinessResponse{Signal: "red", Message: "Past due date"}},
		{"due today", "2026-10-10", model.ReadinessResponse{Signal: "yellow", Message: "Due today"}},
		{"due tomorrow", "2026-10-11", model.ReadinessResponse{Signal: "yellow", Message: "Due in 1 day"}},
		{"due in 3 days", "2026-10-13", model.ReadinessResponse{Signal: "yellow", Message: "Due in 3 days"}},
		{"due in 4 days", "2026-10-14", model.ReadinessResponse{}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			due := mustTime(t, tc.due+"T00:00:00Z")
			release := &model.ReleaseVersion{Name: "quay-v3.17.5", KonfluxApplication: "quay-3-17", DueDate: &due}
			if got := computeReadiness(release, true, false, now); got != tc.want {
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

	var overviews []model.ReleaseOverview
	getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &overviews)
	want := map[string]bool{
		"quay-v3.17.5": false,
		"quay-v3.17.6": true,
		"quay-v3.18.0": false,
		"quay-v3.18.1": true,
	}
	for _, ov := range overviews {
		if ov.NextInStream != want[ov.Release.Name] {
			t.Errorf("%s: next in stream %v, want %v", ov.Release.Name, ov.NextInStream, want[ov.Release.Name])
		}
	}
	if len(overviews) != len(want) {
		t.Errorf("overviews: got %d, want %d", len(overviews), len(want))
	}

	// Released before ART merged its MR: still polled until the MR leaves
	// opened. Shipped with a merged MR: not polled.
	for _, mr := range []model.ShipmentMR{
		{Version: "quay-v3.18.0", IID: 1, State: "opened", Source: "pointer"},
		{Version: "quay-v3.17.5", IID: 2, State: "merged", Source: "pointer"},
	} {
		if err := srv.db.UpsertShipmentMR(ctx, &mr); err != nil {
			t.Fatalf("upsert shipment MR: %v", err)
		}
	}
	polled, err := srv.ShipmentVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"quay-v3.17.6", "quay-v3.18.0", "quay-v3.18.1"}; !slices.Equal(polled, want) {
		t.Errorf("polled %v, want %v", polled, want)
	}

	getJSON(t, srv, "/api/v1/releases/overview", http.StatusOK, &overviews)
	for _, ov := range overviews {
		switch mr := ov.ShipmentMR; ov.Release.Name {
		case "quay-v3.18.0":
			if mr == nil || mr.IID != 1 || mr.Stage != "open" {
				t.Errorf("%s: shipment_mr %+v, want !1 open", ov.Release.Name, mr)
			}
		case "quay-v3.17.6":
			if mr != nil {
				t.Errorf("%s: shipment_mr %+v, want null", ov.Release.Name, mr)
			}
		}
	}
	for version, iid := range map[string]int{"quay-v3.17.5": 2, "quay-v3.17.6": 0} {
		var rel struct {
			ShipmentMR *model.ShipmentMR `json:"shipment_mr"`
		}
		getJSON(t, srv, "/api/v1/releases/"+version, http.StatusOK, &rel)
		if (rel.ShipmentMR == nil) != (iid == 0) || iid != 0 && rel.ShipmentMR.IID != iid {
			t.Errorf("%s: shipment_mr %+v, want iid %d", version, rel.ShipmentMR, iid)
		}
	}
}
