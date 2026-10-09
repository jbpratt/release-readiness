package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchIssues(t *testing.T) {
	issues := []Issue{
		{
			Key: "PROJQUAY-100",
			Fields: IssueFields{
				Summary:   "Fix auth bug",
				Status:    StatusField{Name: "Closed"},
				Priority:  PriorityField{Name: "Major"},
				Labels:    []string{"qe-approved"},
				Assignee:  &UserField{DisplayName: "Jane Doe"},
				IssueType: TypeField{Name: "Bug"},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}

		// Check Basic Auth header
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("test@example.com:test-token"))
		if r.Header.Get("Authorization") != wantAuth {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}

		resp := searchResponse{
			Issues: issues,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL: srv.URL,
		Email:   "test@example.com",
		Token:   "test-token",
		Project: "PROJQUAY",
	})
	client.minDelay = 0 // disable delay for tests

	result, err := client.SearchIssues(context.Background(), "3.16.2")
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d issues, want 1", len(result))
	}
	if result[0].Key != "PROJQUAY-100" {
		t.Errorf("key: got %q, want PROJQUAY-100", result[0].Key)
	}
	if result[0].Fields.Status.Name != "Closed" {
		t.Errorf("status: got %q, want Closed", result[0].Fields.Status.Name)
	}
}

func TestGetVersion(t *testing.T) {
	versions := []VersionField{
		{Name: "3.16.1", Released: true},
		{Name: "3.16.2", ReleaseDate: "2026-02-20", Released: false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/project/PROJQUAY/versions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(versions)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL: srv.URL,
		Email:   "test@example.com",
		Token:   "test-token",
		Project: "PROJQUAY",
	})
	client.minDelay = 0

	v, err := client.GetVersion(context.Background(), "3.16.2")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.Name != "3.16.2" {
		t.Errorf("name: got %q, want 3.16.2", v.Name)
	}
	if v.Released {
		t.Error("released: got true, want false")
	}

	// Test version not found
	_, err = client.GetVersion(context.Background(), "99.99.99")
	if err == nil {
		t.Error("expected error for non-existent version")
	}
}

func TestSearchIssuesPagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		token := r.URL.Query().Get("nextPageToken")

		var resp searchResponse
		if token == "" {
			resp = searchResponse{
				NextPageToken: "page2",
				Issues: []Issue{
					{Key: "PROJ-1"},
					{Key: "PROJ-2"},
				},
			}
		} else {
			resp = searchResponse{
				Issues: []Issue{
					{Key: "PROJ-3"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Project: "PROJ"})
	client.minDelay = 0
	result, err := client.SearchIssues(context.Background(), "1.0")
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d issues, want 3", len(result))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", callCount)
	}
}

func TestDiscoverActiveReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		jql := r.URL.Query().Get("jql")
		if jql == "" {
			t.Error("expected JQL parameter")
		}

		resp := searchResponse{
			Issues: []Issue{
				{
					Key: "PROJQUAY-10276",
					Fields: IssueFields{
						Summary: "Release Quay v3.16.2",
						Status:  StatusField{Name: "In Progress"},
						DueDate: "2026-02-28",
					},
				},
				{
					Key: "PROJQUAY-10170",
					Fields: IssueFields{
						Summary: "Release Quay v3.17.0",
						Status:  StatusField{Name: "New"},
						DueDate: "2026-03-15",
					},
				},
				{
					Key: "PROJQUAY-10278",
					Fields: IssueFields{
						Summary: "Release OMR v2.0.10",
						Status:  StatusField{Name: "Testing"},
						DueDate: "2026-02-20",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL: srv.URL,
		Email:   "test@example.com",
		Token:   "test-token",
		Project: "PROJQUAY",
	})
	client.minDelay = 0

	releases, err := client.DiscoverActiveReleases(context.Background())
	if err != nil {
		t.Fatalf("DiscoverActiveReleases: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("got %d releases, want 3", len(releases))
	}

	// Check Quay release
	if releases[0].FixVersion != "quay-v3.16.2" {
		t.Errorf("release[0].FixVersion: got %q, want quay-v3.16.2", releases[0].FixVersion)
	}
	if releases[0].ReleaseTicketKey != "PROJQUAY-10276" {
		t.Errorf("release[0].ReleaseTicketKey: got %q, want PROJQUAY-10276", releases[0].ReleaseTicketKey)
	}
	if releases[0].KonfluxApplication != "quay-3-16" {
		t.Errorf("release[0].KonfluxApplication: got %q, want quay-3-16", releases[0].KonfluxApplication)
	}
	if releases[0].DueDate == nil {
		t.Fatal("release[0].DueDate: got nil, want 2026-02-28")
	}
	if releases[0].DueDate.Format("2006-01-02") != "2026-02-28" {
		t.Errorf("release[0].DueDate: got %s, want 2026-02-28", releases[0].DueDate.Format("2006-01-02"))
	}

	// Check second Quay release
	if releases[1].FixVersion != "quay-v3.17.0" {
		t.Errorf("release[1].FixVersion: got %q, want quay-v3.17.0", releases[1].FixVersion)
	}
	if releases[1].KonfluxApplication != "quay-3-17" {
		t.Errorf("release[1].KonfluxApplication: got %q, want quay-3-17", releases[1].KonfluxApplication)
	}

	// Check OMR release
	if releases[2].FixVersion != "omr-v2.0.10" {
		t.Errorf("release[2].FixVersion: got %q, want omr-v2.0.10", releases[2].FixVersion)
	}
	if releases[2].KonfluxApplication != "omr-2-0" {
		t.Errorf("release[2].KonfluxApplication: got %q, want omr-2-0", releases[2].KonfluxApplication)
	}
}

func TestParseVersionFromSummary(t *testing.T) {
	tests := []struct {
		summary     string
		wantProduct string
		wantVersion string
		wantOK      bool
	}{
		{"Release Quay v3.16.2", "quay", "3.16.2", true},
		{"Release Quay v3.17.0", "quay", "3.17.0", true},
		{"Release OMR v2.0.10", "omr", "2.0.10", true},
		{"Release Quay v3.9.18", "quay", "3.9.18", true},
		{"⦗konflux⦘ Quay v3.15.3", "quay", "3.15.3", true},
		{"Release Quay v3.15.4", "quay", "3.15.4", true},
		{"Release Quay v3.12.14", "quay", "3.12.14", true},
		{"no version here", "", "", false},
		{"Investigate why PROJQUAY-10909 fix was not included in 3.17.3 advisory and release notes", "", "", false},
	}

	for _, tc := range tests {
		product, version, ok := ParseVersionFromSummary(tc.summary)
		if ok != tc.wantOK {
			t.Errorf("ParseVersionFromSummary(%q): ok=%v, want %v", tc.summary, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if product != tc.wantProduct {
			t.Errorf("ParseVersionFromSummary(%q): product=%q, want %q", tc.summary, product, tc.wantProduct)
		}
		if version != tc.wantVersion {
			t.Errorf("ParseVersionFromSummary(%q): version=%q, want %q", tc.summary, version, tc.wantVersion)
		}
	}
}

func TestFixVersionToKonfluxApp(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"quay-v3.18.2", "quay-3-18"},
		{"quay-v3.18.0", "quay-3-18"},
		{"quay-v5.0.1", "quay-5-0"},
		{"omr-v2.0.10", "omr-2-0"},
		{"omr-v1.5.3", "omr-1-5"},
		{"invalid", ""},
	}

	for _, tc := range tests {
		got := FixVersionToKonfluxApp(tc.input)
		if got != tc.want {
			t.Errorf("FixVersionToKonfluxApp(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStreamVersion(t *testing.T) {
	for in, want := range map[string]string{
		"quay-v3.16.2":  "quay-v3.16.z",
		"omr-v2.0.10":   "omr-v2.0.z",
		"quay-v3.16.z":  "",
		"3.16.2":        "",
		"quay-v3.16.2x": "",
	} {
		if got := StreamVersion(in); got != want {
			t.Errorf("StreamVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildSearchJQL(t *testing.T) {
	client := New(Config{Project: "PROJQUAY"})
	got := client.buildSearchJQL("quay-v3.16.2")
	want := `project=PROJQUAY AND "Target Version"="quay-v3.16.2"`
	if got != want {
		t.Errorf("buildSearchJQL:\n got %q\nwant %q", got, want)
	}
}

func TestSearchIssuesTargetVersion(t *testing.T) {
	var capturedJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedJQL = r.URL.Query().Get("jql")
		resp := searchResponse{
			Issues: []Issue{{Key: "PROJQUAY-10157"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL: srv.URL,
		Project: "PROJQUAY",
	})
	client.minDelay = 0

	result, err := client.SearchIssues(context.Background(), "quay-v3.17.0")
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d issues, want 1", len(result))
	}

	wantJQL := `project=PROJQUAY AND "Target Version"="quay-v3.17.0"`
	if capturedJQL != wantJQL {
		t.Errorf("JQL:\n got %q\nwant %q", capturedJQL, wantJQL)
	}
}

func TestRateLimitRetry(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		resp := searchResponse{
			Issues: []Issue{{Key: "PROJ-1"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Project: "PROJ"})
	client.minDelay = 0

	result, err := client.SearchIssues(context.Background(), "1.0")
	if err != nil {
		t.Fatalf("SearchIssues after retries: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d issues, want 1", len(result))
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 retries + 1 success), got %d", callCount)
	}
}

func TestAnonymousFallbackIsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seraph-LoginReason", "AUTHENTICATED_FAILED")
		_ = json.NewEncoder(w).Encode(searchResponse{})
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "bogus", Project: "PROJ"})
	client.minDelay = 0

	if _, err := client.SearchIssues(context.Background(), "1.0"); err == nil || !strings.Contains(err.Error(), "returned 401") {
		t.Fatalf("SearchIssues error = %v, want a 401", err)
	}
}

func TestAPIURLSeparateFromSiteURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ex/jira/abc/rest/api/3/search/jql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(searchResponse{})
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL + "/ex/jira/abc", SiteURL: "https://site.example/", Project: "PROJ"})
	client.minDelay = 0
	if _, err := client.SearchIssues(context.Background(), "1.0"); err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if got := client.SiteURL(); got != "https://site.example" {
		t.Errorf("SiteURL: got %q, want https://site.example", got)
	}
	if got := New(Config{BaseURL: "https://jira.example/"}).SiteURL(); got != "https://jira.example" {
		t.Errorf("default SiteURL: got %q, want https://jira.example", got)
	}
}
