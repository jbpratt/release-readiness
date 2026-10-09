package artbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const operatorImage = "quay.io/redhat-user-workloads/ocp-art-tenant/art-images@sha256:bfd08ada78f2c19d7dc773f42fc53f5ceb649ff1c34da95e62fb73f0a15a6563"

type fakeStore struct {
	cands   []Candidate
	got     []Build
	apps    []string
	pending map[string][]PendingBuild
	// attempts and covered are the last StoreArtBuildAttempts call.
	attempts []Attempt
	covered  [2]time.Time
	// searchedAt is when the fake service answered the last search.
	searchedAt, checkedAt time.Time
}

func (f *fakeStore) ListArtBuildCandidates(context.Context, time.Time, int) ([]Candidate, error) {
	return f.cands, nil
}

func (f *fakeStore) UpsertArtBuild(_ context.Context, b Build) error {
	f.got = append(f.got, b)
	return nil
}

var operator = Candidate{
	Image:        operatorImage,
	Component:    "quay-operator", // as stage Snapshots name it
	Applications: []string{"quay-3-18"},
	FirstSeen:    time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
}

func (f *fakeStore) ListActiveApplications(context.Context) ([]string, error) {
	return f.apps, nil
}

func (f *fakeStore) ReplaceArtPendingBuilds(_ context.Context, group string, builds []PendingBuild, checkedAt time.Time) error {
	if f.pending == nil {
		f.pending = map[string][]PendingBuild{}
	}
	f.pending[group] = builds
	f.checkedAt = checkedAt
	return nil
}

func (f *fakeStore) StoreArtBuildAttempts(_ context.Context, _ string, attempts []Attempt, from, to time.Time) error {
	f.attempts, f.covered = attempts, [2]time.Time{from, to}
	return nil
}

func always(fixture string) func(string) string { return func(string) string { return fixture } }

// resolveWith runs one pass over c against a service that answers /search
// with fixture(group) and /build with record.json. It returns the stored
// build and every search query, in order.
func resolveWith(t *testing.T, c Candidate, fixture func(group string) string) (Build, []url.Values) {
	t.Helper()
	var searches []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Errorf("search without XHR header")
			}
			searches = append(searches, r.URL.Query())
			http.ServeFile(w, r, "testdata/"+fixture(r.URL.Query().Get("group")))
		case "/build":
			if q := r.URL.Query(); q.Get("record_id") != "7f4d8117-35dc-6c01-1e27-3b3d32a2fa8f" || q.Get("format") != "json" {
				t.Errorf("build query: %v", q)
			}
			http.ServeFile(w, r, "testdata/record.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := &fakeStore{cands: []Candidate{c}}
	r := NewResolver(NewClient(srv.URL+"/", srv.Client()), store, slog.Default())
	r.gap = time.Millisecond
	r.ResolveOnce(t.Context())
	if len(store.got) != 1 {
		t.Fatalf("stored %d builds, want 1", len(store.got))
	}
	return store.got[0], searches
}

func groups(searches []url.Values) []string {
	var gs []string
	for _, q := range searches {
		gs = append(gs, q.Get("group"))
	}
	return gs
}

func TestResolveMatch(t *testing.T) {
	b, searches := resolveWith(t, operator, always("search_match.json"))
	want := Build{
		Digest:       "sha256:bfd08ada78f2c19d7dc773f42fc53f5ceb649ff1c34da95e62fb73f0a15a6563",
		State:        StateResolved,
		NVR:          "quay-operator-container-3.18.1-202609300827.p2.g35cf767.assembly.stream.el9",
		RecordID:     "7f4d8117-35dc-6c01-1e27-3b3d32a2fa8f",
		UpstreamRepo: "https://github.com/quay/quay-operator",
		UpstreamSHA:  "35cf767efc3b4e7bebc7802afcf12b60cd343bc0",
	}
	b.CheckedAt = time.Time{}
	if b != want {
		t.Errorf("build:\n got %+v\nwant %+v", b, want)
	}
	if len(searches) != 1 {
		t.Fatalf("searches: got %d, want 1", len(searches))
	}
	for k, v := range map[string]string{
		"image_sha_tag": "bfd08ada78f2c19d7dc773f42fc53f5ceb649ff1c34da95e62fb73f0a15a6563",
		"group":         "quay-3.18",
		"assembly":      "stream",
		"dateRange":     "2026-06-03 to 2026-10-02",
	} {
		if got := searches[0].Get(k); got != v {
			t.Errorf("search %s: got %q, want %q", k, got, v)
		}
	}
}

func TestResolveNoMatch(t *testing.T) {
	b, _ := resolveWith(t, operator, always("search_none.json"))
	if b.State != StateUnresolved || b.NVR != "" || b.CheckedAt.IsZero() {
		t.Errorf("build: got %+v, want unresolved with a check time", b)
	}
}

// A pending row of the same NVR has no image and must not shadow the finished build.
func TestResolvePendingShadow(t *testing.T) {
	b, _ := resolveWith(t, operator, always("search_pending.json"))
	if b.State != StateResolved || b.RecordID != "7f4d8117-35dc-6c01-1e27-3b3d32a2fa8f" {
		t.Errorf("build: got %+v, want the finished record", b)
	}
}

// quay-images-base names the stream only in its component names.
func TestResolveBaseImageGroup(t *testing.T) {
	c := operator
	c.Applications, c.Component = []string{"quay-images-base"}, "quay-3-9-base-rhel9"
	_, searches := resolveWith(t, c, always("search_none.json"))
	if got := groups(searches); !slices.Equal(got, []string{"quay-3.9"}) {
		t.Errorf("search groups: got %v, want [quay-3.9]", got)
	}
}

// An image held by two streams is searched in each until one has the build.
func TestResolveOtherStream(t *testing.T) {
	c := operator
	c.Applications = []string{"quay-3-16", "quay-3-18"}
	b, searches := resolveWith(t, c, func(group string) string {
		if group == "quay-3.18" {
			return "search_match.json"
		}
		return "search_none.json"
	})
	if got := groups(searches); !slices.Equal(got, []string{"quay-3.16", "quay-3.18"}) {
		t.Errorf("search groups: got %v, want [quay-3.16 quay-3.18]", got)
	}
	if b.State != StateResolved {
		t.Errorf("build: got %+v, want resolved", b)
	}
}

// refreshWith runs one pass with no image candidates against a service whose
// /search answers with the fixture file, or 502 when fixture is empty.
func refreshWith(t *testing.T, fixture string) (*fakeStore, []url.Values) {
	t.Helper()
	var searches []url.Values
	store := &fakeStore{apps: []string{"quay-3-18", "fbc-quay-3-18", "quay-images-base"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches = append(searches, r.URL.Query())
		store.searchedAt = time.Now()
		if fixture == "" {
			http.Error(w, "timeout", http.StatusBadGateway)
			return
		}
		http.ServeFile(w, r, fixture)
	}))
	defer srv.Close()

	r := NewResolver(NewClient(srv.URL, srv.Client()), store, slog.Default())
	r.gap = time.Millisecond
	r.ResolveOnce(t.Context())
	return store, searches
}

// Each active stream is searched once, and its newest image build per NVR
// name and version is stored while it runs. quay-operator's and quay-clair's
// newest builds have finished, so neither is stored.
func TestRefreshPending(t *testing.T) {
	store, searches := refreshWith(t, "testdata/search_group.json")
	if len(searches) != 1 {
		t.Fatalf("searches: got %d, want 1", len(searches))
	}
	if q := searches[0]; q.Get("group") != "quay-3.18" || q.Get("assembly") != "stream" || q.Has("image_sha_tag") || q.Get("dateRange") == "" {
		t.Errorf("search query: %v", q)
	}
	want := []PendingBuild{
		{
			Version: "3.18.1", Name: "quay-quay-container",
			NVR: "quay-quay-container-3.18.1-202610081800.p2.gbbbbbbb.assembly.stream.el9", RecordID: "rec-newer",
			UpstreamSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			StartedAt:   time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC),
		},
		{
			Version: "3.18.2", Name: "quay-quay-container",
			NVR: "quay-quay-container-3.18.2-202610081700.p2.gccccccc.assembly.stream.el9", RecordID: "rec-z2",
			UpstreamSHA: "cccccccccccccccccccccccccccccccccccccccc",
			StartedAt:   time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC),
		},
	}
	if got, ok := store.pending["quay-3.18"]; !ok || !slices.Equal(got, want) {
		t.Errorf("pending:\n got %+v\nwant %+v", store.pending, want)
	}
	if store.checkedAt.Before(store.searchedAt) {
		t.Errorf("checkedAt %v is before the search at %v", store.checkedAt, store.searchedAt)
	}
}

// A failed search leaves the stored pending builds alone.
func TestRefreshPendingFailure(t *testing.T) {
	store, searches := refreshWith(t, "")
	if len(searches) != 1 || store.pending != nil {
		t.Errorf("searches %d, replaced %+v; want 1 search and no replace", len(searches), store.pending)
	}
}

// Every image record of the search is stored, and the search covers its
// whole window.
func TestRefreshAttempts(t *testing.T) {
	start := time.Now()
	store, _ := refreshWith(t, "testdata/search_group.json")
	if len(store.attempts) != 8 {
		t.Fatalf("attempts: got %d, want the 8 image records", len(store.attempts))
	}
	want := Attempt{
		Version: "3.18.1", Name: "quay-operator-container",
		NVR: "quay-operator-container-3.18.1-202609300827.p2.g35cf767.assembly.stream.el9", RecordID: "7f4d8117-35dc-6c01-1e27-3b3d32a2fa8f",
		Outcome:   "success",
		StartedAt: time.Date(2026, 9, 30, 8, 36, 21, 0, time.UTC),
	}
	if !slices.Contains(store.attempts, want) {
		t.Errorf("attempts %+v lack %+v", store.attempts, want)
	}
	if from, to := store.covered[0], store.covered[1]; from.Before(start.AddDate(0, 0, -pendingDays)) || from.After(store.searchedAt.AddDate(0, 0, -pendingDays)) || to.Before(store.searchedAt) {
		t.Errorf("covered [%v, %v], want from %d days before the pass to the search", from, to, pendingDays)
	}
}

// A capped answer covers only from its oldest row.
func TestRefreshAttemptsCapped(t *testing.T) {
	oldest := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	builds := make([]map[string]string, searchCap)
	for i := range builds {
		builds[i] = map[string]string{
			"type": "fbc", "outcome": "success", "record_id": fmt.Sprint(i),
			"nvr":        "quay-fbc-3.18.1-1.el9",
			"start_time": oldest.Add(time.Duration(searchCap-i) * time.Minute).Format(time.RFC1123),
		}
	}
	builds[searchCap-1]["start_time"] = oldest.Format(time.RFC1123)
	body, err := json.Marshal(map[string]any{"builds": builds})
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "capped.json")
	if err := os.WriteFile(fixture, body, 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := refreshWith(t, fixture)
	if !store.covered[0].Equal(oldest) {
		t.Errorf("covered from %v, want the oldest row %v", store.covered[0], oldest)
	}
}
