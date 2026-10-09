package jira

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func TestSyncOnceReportsAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, Token: "bogus", Project: "PROJQUAY"})
	client.minDelay = 0
	reg := syncstatus.New()
	s := NewSyncer(client, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Status = reg.Track("jira", time.Minute)

	s.SyncOnce(context.Background())

	p := reg.Problems(time.Now())
	if len(p) != 1 || !strings.HasPrefix(p[0].Message, "JIRA authentication failed: ") || !strings.Contains(p[0].Message, "returned 401") {
		t.Fatalf("problems = %+v, want a jira auth failure", p)
	}
}

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
				Fields: IssueFields{Summary: "Release Quay v3.16.2", Status: StatusField{Name: "New"}},
			}}})
		case r.URL.Path == "/rest/api/3/search/jql":
			w.Header().Set("X-Seraph-LoginReason", "AUTHENTICATED_FAILED")
			_ = json.NewEncoder(w).Encode(searchResponse{})
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
	// The version lookup 404s too, but a missing fixVersion is not sync health.
	p := reg.Problems(time.Now())
	if len(p) != 1 || !strings.HasPrefix(p[0].Message, "JIRA authentication failed: search issues") || strings.Contains(p[0].Message, "errors this pass") {
		t.Fatalf("problems = %+v, want one jira auth failure from search", p)
	}
}

type versionStore struct {
	Store
	versions []model.ReleaseVersion
	synced   []string
}

func (v *versionStore) UpsertReleaseVersion(context.Context, *model.ReleaseVersion) error {
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

	if strings.Join(store.synced, ",") != "quay-v3.17.4,quay-v3.17.5" {
		t.Errorf("synced = %v, want [quay-v3.17.4 quay-v3.17.5]", store.synced)
	}
}
