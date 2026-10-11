package artbuild

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"
)

const operatorImage = "quay.io/redhat-user-workloads/ocp-art-tenant/art-images@sha256:bfd08ada78f2c19d7dc773f42fc53f5ceb649ff1c34da95e62fb73f0a15a6563"

type fakeStore struct {
	cands []Candidate
	got   []Build
	// fbcs is the stored FBC records, by digest.
	fbcs map[string]FBCRecord
}

func (f *fakeStore) ListArtBuildCandidates(context.Context, time.Time, int) ([]Candidate, error) {
	return f.cands, nil
}

func (f *fakeStore) UpsertArtBuild(_ context.Context, b Build) error {
	f.got = append(f.got, b)
	return nil
}

func (f *fakeStore) ListUnreadFBCImages(_ context.Context, limit int) ([]Build, error) {
	var unread []Build
	for _, b := range f.got {
		_, _, _, fbc := ParseFBC(b.NVR)
		if _, read := f.fbcs[b.Digest]; fbc && !read && len(unread) < limit {
			unread = append(unread, b)
		}
	}
	return unread, nil
}

func (f *fakeStore) UpsertFBCRecord(_ context.Context, r FBCRecord) error {
	if f.fbcs == nil {
		f.fbcs = map[string]FBCRecord{}
	}
	f.fbcs[r.Digest] = r
	return nil
}

var operator = Candidate{
	Image:        operatorImage,
	Component:    "quay-operator", // as stage Snapshots name it
	Applications: []string{"quay-3-18"},
	FirstSeen:    time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
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
	if len(store.fbcs) != 0 {
		t.Errorf("stored FBC records %v, want none: not an FBC image", store.fbcs)
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

// The FBC image of fbc_record.json.
const (
	fbcImageNVR = "quay-operator-fbc-3.18.1-20261009044841.ocp4.21"
	fbcDigest   = "sha256:4de723583d790cd4d680216f6fb88bd564189e3b18bfb8b0b49b7f45ff589425"
	fbcImage    = "quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc@" + fbcDigest
)

// fbcService is an ART build history service whose one build is the FBC image
// of fbc_record.json, its outcome set to outcome. /search lists the record as
// its row. It returns a client of it and the count of requests it answered.
func fbcService(t *testing.T, outcome string) (*Client, *int) {
	t.Helper()
	rec, err := os.ReadFile("testdata/fbc_record.json")
	if err != nil {
		t.Fatal(err)
	}
	rec = bytes.Replace(rec, []byte(`"outcome": "success"`), []byte(`"outcome": "`+outcome+`"`), 1)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/search":
			_, _ = fmt.Fprintf(w, `{"builds": [%s], "warnings": []}`, rec)
		case "/build":
			if q := r.URL.Query(); q.Get("nvr") != fbcImageNVR || q.Get("record_id") != "875a2a69-8e3e-9e13-4432-fe10e617c1f3" {
				t.Errorf("build query: %v", q)
			}
			_, _ = w.Write(rec)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, srv.Client()), &requests
}

func resolveOnce(t *testing.T, client *Client, store *fakeStore) {
	r := NewResolver(client, store, slog.Default())
	r.gap = time.Millisecond
	r.ResolveOnce(t.Context())
}

// Resolving an FBC image stores the bundles it holds, from the /build read
// that resolves it.
func TestResolveFBC(t *testing.T) {
	client, requests := fbcService(t, "success")
	store := &fakeStore{cands: []Candidate{{
		Image:        fbcImage,
		Component:    "fbc-quay-3-18-quay-operator",
		Applications: []string{"fbc-quay-3-18"},
		FirstSeen:    time.Date(2026, 10, 9, 5, 17, 4, 0, time.UTC),
	}}}
	resolveOnce(t, client, store)
	if len(store.got) != 1 || store.got[0].State != StateResolved || store.got[0].NVR != fbcImageNVR {
		t.Fatalf("builds = %+v, want %s resolved", store.got, fbcImageNVR)
	}
	want := map[string]FBCRecord{fbcDigest: {
		Digest:     fbcDigest,
		Pullspec:   fbcImage,
		EndTime:    "2026-10-09T06:14:00",
		BundleNVRs: []string{"quay-operator-metadata-container-3.18.1.202610090339.p2.g35cf767.assembly.stream.el9-1"},
	}}
	if !reflect.DeepEqual(store.fbcs, want) {
		t.Errorf("FBC records = %+v, want %+v", store.fbcs, want)
	}
	if *requests != 2 {
		t.Errorf("requests = %d, want 2: the search and the /build read", *requests)
	}
}

// An FBC image resolved without its FBC record has its /build record read
// once; a failed build holds no bundle.
func TestReadFBCRecords(t *testing.T) {
	client, requests := fbcService(t, "failure")
	store := &fakeStore{got: []Build{{Digest: fbcDigest, State: StateResolved, NVR: fbcImageNVR, RecordID: "875a2a69-8e3e-9e13-4432-fe10e617c1f3"}}}
	resolveOnce(t, client, store)
	want := map[string]FBCRecord{fbcDigest: {Digest: fbcDigest, Pullspec: fbcImage, EndTime: "2026-10-09T06:14:00", BundleNVRs: []string{}}}
	if !reflect.DeepEqual(store.fbcs, want) || *requests != 1 {
		t.Errorf("FBC records = %+v after %d requests, want %+v after 1", store.fbcs, *requests, want)
	}
	resolveOnce(t, client, store)
	if *requests != 1 {
		t.Errorf("requests after a second pass = %d, want still 1", *requests)
	}
}
