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

// The ticket list is the release's Target Version tickets plus the .z stream
// tickets a commit of its candidate names, since the previous version's
// candidate when that one is unshipped, each listed once.
func TestGetBuildTickets(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	for _, v := range []model.ReleaseVersion{
		{Name: "quay-v3.18.0", Archived: true},
		{Name: "quay-v3.18.1"},
		{Name: "quay-v3.18.2"},
		{Name: "quay-v3.18.3"},
	} {
		v.KonfluxApplication = "quay-3-18"
		if err := srv.db.UpsertReleaseVersion(ctx, &v); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range []model.JiraIssueRecord{
		{Key: "PROJQUAY-1", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-1", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-2", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-3", FixVersion: "quay-v3.18.1"},
		{Key: "PROJQUAY-4", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-5", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-6", FixVersion: "quay-v3.18.z"},
		{Key: "PROJQUAY-7", FixVersion: "quay-v3.18.0"},
		{Key: "PROJQUAY-8", FixVersion: "quay-v3.18.z"},
	} {
		if err := srv.db.UpsertJiraIssue(ctx, &i); err != nil {
			t.Fatal(err)
		}
	}

	// 3.18.1's STAGE build, and 3.18.2's build: a newer quay beside the same
	// clair.
	seedImages(t, srv, "stage-image", "2026-10-07T20:02:43Z", map[string]string{"quay-3-18-quay-quay": testImage("stage/quay"), "quay-3-18-quay-clair": testImage("clair")})
	if err := srv.db.UpsertStagedSnapshot(ctx, "stage-image", "3.18.1", "image", "stage", mustTime(t, "2026-10-07T20:02:43Z")); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "stage-release", "stage-image", "quay-advisory-stage-3-18", "Succeeded", "2026-10-07T20:02:43Z", "2026-10-07T20:14:57Z")
	seedImages(t, srv, "quay-3-18-20261010-013713-000", "2026-10-10T01:48:34Z", map[string]string{"quay-3-18-quay-quay": testImage("new/quay"), "quay-3-18-quay-clair": testImage("clair")})
	quay1, quay2, clair := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("c", 40)
	for _, b := range []artbuild.Build{
		{Digest: testDigest("stage/quay"), UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: quay1},
		{Digest: testDigest("new/quay"), NVR: testNVR("quay-quay", "3.18.2"), UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: quay2},
		{Digest: testDigest("clair"), UpstreamRepo: "https://github.com/quay/clair", UpstreamSHA: clair},
	} {
		b.State, b.CheckedAt = artbuild.StateResolved, time.Now()
		if err := srv.db.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	// quay's default branch has commits 3, 4, 6 and 7, which 3.18.1's
	// build lacks; the newest build adds commit 5 to it and merges in
	// commit 6.
	commit := func(n int) model.BuildCommit {
		sha := fmt.Sprintf("%040x", n)
		return model.BuildCommit{Component: "quay-3-18-quay-quay", CommitSHA: sha, CommitURL: "https://github.com/quay/quay/commit/" + sha}
	}
	messages := map[int]string{1: "PROJQUAY-1: fix", 2: "PROJQUAY-4: backport", 3: "PROJQUAY-2: fix", 4: "PROJQUAY-1: follow-up", 5: "PROJQUAY-5: backport", 6: "PROJQUAY-8: fix", 7: "PROJQUAY-8: follow-up"}
	compares := map[string][]int{
		"/repos/quay/quay/compare/HEAD..." + quay1:          {1, 2},
		"/repos/quay/quay/compare/" + quay1 + "...HEAD":     {3, 4, 6, 7},
		"/repos/quay/quay/compare/HEAD..." + quay2:          {1, 2, 5},
		"/repos/quay/quay/compare/" + quay1 + "..." + quay2: {5, 6},
		"/repos/quay/quay/compare/" + quay2 + "...HEAD":     {3, 4, 7},
		"/repos/quay/clair/compare/HEAD..." + clair:         {},
		"/repos/quay/clair/compare/" + clair + "...HEAD":    {},
	}
	var requested []string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		ns, ok := compares[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		commits := []map[string]any{}
		for _, n := range ns {
			c := commit(n)
			commits = append(commits, map[string]any{"sha": c.CommitSHA, "html_url": c.CommitURL, "commit": map[string]string{"message": messages[n]}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ahead", "total_commits": len(commits), "commits": commits})
	}))
	defer gh.Close()
	srv.Scanner = github.NewScanner(github.NewClient(gh.URL, "", gh.Client()), srv.TicketBuilds, slog.Default())
	srv.Scanner.ScanOnce(ctx)
	// 3.18.2 has no STAGE build: its newest build's quay is compared, and
	// since 3.18.1's, but not with the default branch, as it has no Target
	// Version tickets. The archived 3.18.0, and 3.18.3, which has no build,
	// are not scanned.
	slices.Sort(requested)
	wantRequested := []string{
		"/repos/quay/clair/compare/" + clair + "...HEAD",
		"/repos/quay/clair/compare/HEAD..." + clair,
		"/repos/quay/quay/compare/" + quay1 + "...HEAD",
		"/repos/quay/quay/compare/" + quay1 + "..." + quay2,
		"/repos/quay/quay/compare/HEAD..." + quay1,
		"/repos/quay/quay/compare/HEAD..." + quay2,
	}
	slices.Sort(wantRequested)
	if !slices.Equal(requested, wantRequested) {
		t.Errorf("compares = %v, want %v", requested, wantRequested)
	}
	// Given a Target Version ticket, 3.18.2's build is compared with the
	// default branch on the next pass.
	if err := srv.db.UpsertJiraIssue(ctx, &model.JiraIssueRecord{Key: "PROJQUAY-9", FixVersion: "quay-v3.18.2"}); err != nil {
		t.Fatal(err)
	}
	srv.Scanner.ScanOnce(ctx)

	type row struct {
		key, fixVersion     string
		inBuild, notInBuild []model.BuildCommit
	}
	check := func(version string, build *model.TicketBuild, zSince string, want []row) {
		t.Helper()
		var got model.BuildTickets
		getJSON(t, srv, "/api/v1/releases/"+version+"/build-tickets", http.StatusOK, &got)
		if !reflect.DeepEqual(got.Build, build) || got.Reason != "" || got.ZSince != zSince || len(got.NotCompared) != 0 {
			t.Errorf("%s: build %+v, reason %q, z_since %q, not compared %+v; want %+v, z_since %q", version, got.Build, got.Reason, got.ZSince, got.NotCompared, build, zSince)
		}
		if len(got.Tickets) != len(want) {
			t.Fatalf("%s: tickets = %+v, want %d", version, got.Tickets, len(want))
		}
		for i, w := range want {
			if g := got.Tickets[i]; g.Key != w.key || g.FixVersion != w.fixVersion || !slices.Equal(g.InBuild, w.inBuild) || !slices.Equal(g.NotInBuild, w.notInBuild) {
				t.Errorf("%s: tickets[%d] = %s %s %+v %+v, want %+v", version, i, g.Key, g.FixVersion, g.InBuild, g.NotInBuild, w)
			}
		}
	}
	staged := &model.TicketBuild{Snapshot: "stage-image", Source: "staged"}
	newest := &model.TicketBuild{Snapshot: "quay-3-18-20261010-013713-000", Source: "newest"}
	// No version before 3.18.1 is unshipped, so its .z tickets are those its
	// build's commits name. PROJQUAY-1 is listed once, as 3.18.1's, and in
	// the build despite the default branch's follow-up; PROJQUAY-2 is fixed
	// only on the default branch; PROJQUAY-6 is a .z ticket no commit names.
	check("quay-v3.18.1", staged, "", []row{
		{"PROJQUAY-1", "quay-v3.18.1", []model.BuildCommit{commit(1)}, nil},
		{"PROJQUAY-2", "quay-v3.18.1", nil, []model.BuildCommit{commit(3)}},
		{"PROJQUAY-3", "quay-v3.18.1", nil, nil},
		{"PROJQUAY-4", "quay-v3.18.z", []model.BuildCommit{commit(2)}, nil},
	})
	// 3.18.2's .z tickets are only those named since 3.18.1's build.
	// PROJQUAY-8 came with the merge, so no In build commit names it, but a
	// .z ticket is never red, even when a default-branch commit the build
	// lacks names it.
	check("quay-v3.18.2", newest, "quay-v3.18.1", []row{
		{"PROJQUAY-9", "quay-v3.18.2", nil, nil},
		{"PROJQUAY-5", "quay-v3.18.z", []model.BuildCommit{commit(5)}, nil},
		{"PROJQUAY-8", "quay-v3.18.z", nil, nil},
	})
	// 3.18.3 has no build, so nothing is checked against one.
	var none model.BuildTickets
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.3/build-tickets", http.StatusOK, &none)
	if none.Build != nil || none.Reason != "no build of 3.18.3 yet" || len(none.Tickets) != 0 {
		t.Errorf("3.18.3: got %+v", none)
	}

	var raw struct {
		Build       json.RawMessage `json:"build"`
		Reason      string          `json:"reason"`
		NotCompared json.RawMessage `json:"not_compared"`
		Tickets     []struct {
			Key        string          `json:"key"`
			InBuild    json.RawMessage `json:"in_build"`
			NotInBuild json.RawMessage `json:"not_in_build"`
		} `json:"tickets"`
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.0/build-tickets", http.StatusOK, &raw)
	if string(raw.Build) != "null" || raw.Reason != "archived" || len(raw.Tickets) != 1 {
		t.Errorf("archived: got %+v", raw)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/build-tickets", http.StatusNotFound, nil)

	// Once 3.18.1 ships, it has no build, and 3.18.2's .z tickets are again
	// those its build's commits name.
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true}); err != nil {
		t.Fatal(err)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/build-tickets", http.StatusOK, &raw)
	if string(raw.Build) != "null" || raw.Reason != "shipped" || string(raw.NotCompared) != "[]" || len(raw.Tickets) != 3 ||
		string(raw.Tickets[1].InBuild) != "[]" || string(raw.Tickets[1].NotInBuild) != "[]" {
		t.Errorf("shipped: got %+v", raw)
	}
	check("quay-v3.18.2", newest, "", []row{
		{"PROJQUAY-9", "quay-v3.18.2", nil, nil},
		{"PROJQUAY-1", "quay-v3.18.z", []model.BuildCommit{commit(1)}, nil},
		{"PROJQUAY-4", "quay-v3.18.z", []model.BuildCommit{commit(2)}, nil},
		{"PROJQUAY-5", "quay-v3.18.z", []model.BuildCommit{commit(5)}, nil},
	})
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
		Key: "PROJQUAY-1", Summary: "fix bug", Status: "Open",
		Priority: "Major", FixVersion: "quay-v3.16.3", IssueType: "Bug",
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
	// Release Pending counts as verified.
	want := model.IssueSummary{Total: 2, Verified: 2}
	if *s170 != want {
		t.Errorf("3.17.0: got %+v, want %+v", *s170, want)
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

	if got := getReadiness(); got.Signal != "yellow" || got.Message != "No build snapshots yet" {
		t.Errorf("without snapshot: got %+v, want yellow/No build snapshots yet", got)
	}

	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-snap-1", time.Now(), "quay-3-16-quay")
	resolveNVR(t, srv, "quay-3-16-snap-1/quay-3-16-quay", testNVR("quay-quay", "3.16.3"))

	if got := getReadiness(); got.Signal != "green" || got.Message != "No open issues" {
		t.Errorf("with snapshot: got %+v, want green/No open issues", got)
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
	if ov := got["quay-v3.18.2"]; ov.LatestBuild != nil || ov.Readiness.Message != "No build snapshots yet" {
		t.Errorf("3.18.2 latest_build = %v, readiness %+v; want none", ov.LatestBuild, ov.Readiness)
	}

	// Once shipped, 3.18.1 keeps it; its newer assembly's stage Release
	// succeeds.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true}); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261010020000", "quay-stage-3-18-1-image-20261010020000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T02:00:00Z", "2026-10-10T02:05:00Z")
	if ov := overviews()["quay-v3.18.1"]; !ov.Shipped || ov.LatestBuild == nil || !ov.LatestBuild.Equal(mustTime(t, "2026-10-10T02:00:00Z")) {
		t.Errorf("shipped 3.18.1 latest_build = %v, want its STAGE build's", ov.LatestBuild)
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
