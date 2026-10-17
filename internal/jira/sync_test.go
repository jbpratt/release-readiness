package jira

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

type deleteRecorder struct {
	Store
	deletes int
}

func (d *deleteRecorder) UpsertReleaseVersion(context.Context, *model.ReleaseVersion) error {
	return nil
}

func (d *deleteRecorder) ListAllReleaseVersions(context.Context) ([]model.ReleaseVersion, error) {
	return nil, nil
}

func (d *deleteRecorder) DeleteJiraIssuesNotIn(context.Context, string, []string) error {
	d.deletes++
	return nil
}

func TestSyncOnceRejectedTokenDeletesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Query().Get("jql"), "-area/release"):
			_ = json.NewEncoder(w).Encode(searchResponse{Issues: []Issue{{
				Key:    "PROJQUAY-1",
				Fields: IssueFields{Summary: "Release Quay v3.16.2"},
			}}})
		case r.URL.Path == "/rest/api/3/search/jql":
			w.Header().Set("X-Seraph-LoginReason", "AUTHENTICATED_FAILED")
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case r.URL.Path == "/rest/api/3/project/PROJQUAY/versions":
			_ = json.NewEncoder(w).Encode([]VersionField{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "bogus", Project: "PROJQUAY"})
	client.minDelay = 0
	store := &deleteRecorder{}
	withTx := func(ctx context.Context, fn func(Store) error) error { return fn(store) }
	reg := syncstatus.New()
	s := NewSyncer(client, store, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Status = reg.Track("jira", time.Minute)

	s.SyncOnce(context.Background())

	if store.deletes != 0 {
		t.Errorf("deletes = %d, want 0", store.deletes)
	}
	// quay-v3.16.2 is missing from the versions list, which is not sync health.
	p := reg.Problems(time.Now())
	if len(p) != 1 || !strings.HasPrefix(p[0].Message, "JIRA authentication failed: search issues") || strings.Contains(p[0].Message, "errors this pass") {
		t.Fatalf("problems = %+v, want one jira auth failure from search", p)
	}
}

type versionStore struct {
	Store
	versions []model.ReleaseVersion
	synced   []string
	upserted []model.ReleaseVersion
}

func (v *versionStore) UpsertReleaseVersion(_ context.Context, rv *model.ReleaseVersion) error {
	v.upserted = append(v.upserted, *rv)
	return nil
}

func (v *versionStore) ListAllReleaseVersions(context.Context) ([]model.ReleaseVersion, error) {
	return v.versions, nil
}

func (v *versionStore) UpsertJiraIssue(context.Context, *model.JiraIssueRecord) error {
	return nil
}

func (v *versionStore) DeleteJiraIssuesNotIn(_ context.Context, fixVersion string, _ []string) error {
	v.synced = append(v.synced, fixVersion)
	return nil
}

func TestSyncOnceSyncsShippedUnarchivedVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Query().Get("jql"), "-area/release"):
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case r.URL.Path == "/rest/api/3/search/jql":
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case r.URL.Path == "/rest/api/3/project/PROJQUAY/versions":
			_ = json.NewEncoder(w).Encode([]VersionField{
				{Name: "quay-v3.17.4", Released: true},
				{Name: "quay-v3.17.5"},
				{Name: "quay-v3.10.1", Released: true, Archived: true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "t", Project: "PROJQUAY"})
	client.minDelay = 0
	store := &versionStore{versions: []model.ReleaseVersion{
		{Name: "quay-v3.10.1", Released: true, Archived: true},
		{Name: "quay-v3.17.4", Released: true},
		{Name: "quay-v3.17.5"},
	}}
	withTx := func(ctx context.Context, fn func(Store) error) error { return fn(store) }
	s := NewSyncer(client, store, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))

	s.SyncOnce(context.Background())

	if got := strings.Join(store.synced, ","); got != "quay-v3.17.4,quay-v3.17.5" {
		t.Errorf("synced = %s, want the retained versions", got)
	}
}

func TestSyncOnceListsVersionsOncePerPass(t *testing.T) {
	var versionGets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Query().Get("jql"), "-area/release"):
			_ = json.NewEncoder(w).Encode(searchResponse{Issues: []Issue{
				{Key: "PROJQUAY-1", Fields: IssueFields{Summary: "Release Quay v3.17.5", DueDate: "2026-10-30"}},
				{Key: "PROJQUAY-2", Fields: IssueFields{Summary: "Release Quay v3.18.0"}},
				{Key: "PROJQUAY-3", Fields: IssueFields{Summary: "Release OMR v2.0.10"}},
			}})
		case r.URL.Path == "/rest/api/3/search/jql":
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case r.URL.Path == "/rest/api/3/project/PROJQUAY/versions":
			versionGets++
			_ = json.NewEncoder(w).Encode([]VersionField{
				{Name: "quay-v3.17.4", Released: true, ReleaseDate: "2026-09-01"},
				{Name: "quay-v3.17.5", ReleaseDate: "2026-10-20"},
				{Name: "quay-v3.18.0", Released: true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "t", Project: "PROJQUAY"})
	client.minDelay = 0
	store := &versionStore{versions: []model.ReleaseVersion{
		{Name: "quay-v3.10.1", Released: true, Archived: true},
		{Name: "quay-v3.16.9"},
		{Name: "quay-v3.17.4"},
		{Name: "quay-v3.17.5"},
	}}
	withTx := func(ctx context.Context, fn func(Store) error) error { return fn(store) }
	s := NewSyncer(client, store, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))

	s.SyncOnce(context.Background())

	if versionGets != 1 {
		t.Errorf("versions list GETs = %d, want 1", versionGets)
	}
	date := func(s string) *time.Time {
		d, _ := time.Parse(time.DateOnly, s)
		return &d
	}
	// omr-v2.0.10 is missing from the list: stored without version fields.
	// quay-v3.16.9 is missing and inactive: skipped.
	want := []model.ReleaseVersion{
		{Name: "quay-v3.17.5", ReleaseDate: date("2026-10-20"), ReleaseTicketKey: "PROJQUAY-1", KonfluxApplication: "quay-3-17", DueDate: date("2026-10-30")},
		{Name: "quay-v3.18.0", Released: true, ReleaseTicketKey: "PROJQUAY-2", KonfluxApplication: "quay-3-18"},
		{Name: "omr-v2.0.10", ReleaseTicketKey: "PROJQUAY-3", KonfluxApplication: "omr-2-0"},
		{Name: "quay-v3.17.4", Released: true, ReleaseDate: date("2026-09-01")},
	}
	if !reflect.DeepEqual(store.upserted, want) {
		t.Errorf("upserted:\n got %+v\nwant %+v", store.upserted, want)
	}
}

// rowStore keeps release versions by name, overwriting every field on upsert
// as the SQL upsert does.
type rowStore struct {
	versionStore
	rows map[string]model.ReleaseVersion
}

func (r *rowStore) UpsertReleaseVersion(_ context.Context, rv *model.ReleaseVersion) error {
	r.rows[rv.Name] = *rv
	return nil
}

func (r *rowStore) ListAllReleaseVersions(context.Context) ([]model.ReleaseVersion, error) {
	var out []model.ReleaseVersion
	for _, rv := range r.rows {
		out = append(out, rv)
	}
	return out, nil
}

func TestSyncOnceKeepsStoredFlagsWhenVersionsFail(t *testing.T) {
	versions := []VersionField{{Name: "quay-v3.17.5", Released: true, ReleaseDate: "2026-10-20"}}
	var searched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Query().Get("jql"), "-area/release"):
			_ = json.NewEncoder(w).Encode(searchResponse{Issues: []Issue{
				{Key: "PROJQUAY-1", Fields: IssueFields{Summary: "Release Quay v3.17.5"}},
			}})
		case r.URL.Path == "/rest/api/3/search/jql":
			searched = append(searched, r.URL.Query().Get("jql"))
			_ = json.NewEncoder(w).Encode(searchResponse{})
		case r.URL.Path == "/rest/api/3/project/PROJQUAY/versions":
			if versions == nil {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "t", Project: "PROJQUAY"})
	client.minDelay = 0
	store := &rowStore{rows: map[string]model.ReleaseVersion{}}
	withTx := func(ctx context.Context, fn func(Store) error) error { return fn(store) }
	reg := syncstatus.New()
	s := NewSyncer(client, store, withTx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Status = reg.Track("jira", time.Minute)
	ctx := context.Background()

	check := func(pass string, released, archived bool, date string, failing bool) {
		t.Helper()
		got := store.rows["quay-v3.17.5"]
		gotDate := ""
		if got.ReleaseDate != nil {
			gotDate = got.ReleaseDate.Format(time.DateOnly)
		}
		if got.Released != released || got.Archived != archived || gotDate != date {
			t.Errorf("%s: released=%v archived=%v date=%q, want %v %v %q", pass, got.Released, got.Archived, gotDate, released, archived, date)
		}
		if got.ReleaseTicketKey != "PROJQUAY-1" {
			t.Errorf("%s: ticket = %q, want PROJQUAY-1", pass, got.ReleaseTicketKey)
		}
		if p := reg.Problems(time.Now()); (len(p) > 0) != failing {
			t.Errorf("%s: problems = %+v, want failing=%v", pass, p, failing)
		}
	}

	s.SyncOnce(ctx)
	check("pass 1", true, false, "2026-10-20", false)

	versions = nil
	searched = nil
	s.SyncOnce(ctx)
	check("pass 2", true, false, "2026-10-20", true)
	if len(searched) != 1 {
		t.Errorf("pass 2: issue searches = %d, want 1", len(searched))
	}

	versions = []VersionField{{Name: "quay-v3.17.5", Released: true, Archived: true, ReleaseDate: "2026-10-21"}}
	s.SyncOnce(ctx)
	check("pass 3", true, true, "2026-10-21", false)
}
