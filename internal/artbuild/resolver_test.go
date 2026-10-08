package artbuild

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		RebaseRepo:   "https://github.com/openshift-priv/quay-quay-operator",
		RebaseSHA:    "0395c7827e3d5a47b3757abc376275e2139923cf",
		PipelineURL:  "https://konflux-ui.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com/ns/art-quay-tenant/applications/quay-3-18/pipelineruns/quay-3-18-quay-operator-sjpkg",
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
// /search answers with fixture, or 502 when fixture is empty.
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
		http.ServeFile(w, r, "testdata/"+fixture)
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
	store, searches := refreshWith(t, "search_group.json")
	if len(searches) != 1 {
		t.Fatalf("searches: got %d, want 1", len(searches))
	}
	if q := searches[0]; q.Get("group") != "quay-3.18" || q.Get("assembly") != "stream" || q.Has("image_sha_tag") || q.Get("dateRange") == "" {
		t.Errorf("search query: %v", q)
	}
	want := []PendingBuild{
		{
			Group: "quay-3.18", Version: "3.18.1", Name: "quay-quay-container",
			NVR: "quay-quay-container-3.18.1-202610081800.p2.gbbbbbbb.assembly.stream.el9", RecordID: "rec-newer",
			UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			StartedAt: time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC),
		},
		{
			Group: "quay-3.18", Version: "3.18.2", Name: "quay-quay-container",
			NVR: "quay-quay-container-3.18.2-202610081700.p2.gccccccc.assembly.stream.el9", RecordID: "rec-z2",
			UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: "cccccccccccccccccccccccccccccccccccccccc",
			StartedAt: time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC),
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
