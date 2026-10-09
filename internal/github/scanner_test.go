package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

type fakeStore struct {
	build *model.SelectedBuild
}

func (f *fakeStore) ListAllReleaseVersions(context.Context) ([]model.ReleaseVersion, error) {
	return []model.ReleaseVersion{{Name: "quay-v3.18.1"}, {Name: "quay-v3.17.9", Released: true}, {Name: "quay-v3.16.9", Archived: true}}, nil
}

func (f *fakeStore) SelectedStageBuild(_ context.Context, r *model.ReleaseVersion, _ *regexp.Regexp) (*model.SelectedBuild, string, error) {
	if r.Name != "quay-v3.18.1" {
		return nil, "", fmt.Errorf("scanned inactive version %s", r.Name)
	}
	return f.build, "", nil
}

const repoURL = "https://github.com/quay/quay"

var headSHA = strings.Repeat("b", 40)

// build is a STAGE build of two components from one commit, as an operator
// and its bundle are.
func build(repo, sha string) *model.SelectedBuild {
	return &model.SelectedBuild{SnapshotName: "snap", Components: []model.SelectedBuildComponent{
		{Name: "operator", UpstreamRepo: repo, UpstreamSHA: sha},
		{Name: "operator-bundle", UpstreamRepo: repo, UpstreamSHA: sha},
	}}
}

type commitJSON struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
	} `json:"commit"`
}

func commits(n int, msg func(i int) string) []commitJSON {
	out := make([]commitJSON, n)
	for i := range out {
		out[i].SHA = fmt.Sprintf("%040x", i+1)
		out[i].HTMLURL = "https://github.com/quay/quay/commit/" + out[i].SHA
		out[i].Commit.Message = msg(i)
	}
	return out
}

// compareHandler serves HEAD...headSHA as a build branch that diverged from
// the default branch, with total commits of which all are paginated.
func compareHandler(total int, all []commitJSON) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/quay/quay/compare/HEAD..."+headSHA {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		lo, hi := min((page-1)*per, len(all)), min(page*per, len(all))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "diverged", "ahead_by": total, "behind_by": 5, "total_commits": total, "commits": all[lo:hi],
		})
	}
}

// in is the given commits of the compare, as each of build's components
// carries them.
func in(commits ...int) []model.BuildCommit {
	var out []model.BuildCommit
	for _, name := range []string{"operator", "operator-bundle"} {
		for _, i := range commits {
			sha := fmt.Sprintf("%040x", i)
			out = append(out, model.BuildCommit{Component: name, CommitSHA: sha, CommitURL: "https://github.com/quay/quay/commit/" + sha})
		}
	}
	return out
}

func newScanner(url string, hc *http.Client, store Store) *Scanner {
	return NewScanner(NewClient(url, "", hc), store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestScanOnce(t *testing.T) {
	linear := commits(3, func(i int) string {
		return []string{"PROJQUAY-101: fix\n\nAlso PROJQUAY-101 and PROJQUAY-7.", "NO-ISSUE: chore XPROJQUAY-9", "PROJQUAY-7: follow-up"}[i]
	})
	long := commits(150, func(i int) string {
		if i == 140 {
			return "PROJQUAY-140 late"
		}
		return "NO-ISSUE"
	})
	for _, tc := range []struct {
		name     string
		repo     string
		sha      string
		handler  http.HandlerFunc
		found    map[string][]model.BuildCommit
		reason   string
		requests int
		failed   bool
	}{
		{
			name:     "keys in commit messages",
			repo:     repoURL,
			sha:      headSHA,
			handler:  compareHandler(3, linear),
			found:    map[string][]model.BuildCommit{"PROJQUAY-101": in(1), "PROJQUAY-7": in(1, 3)},
			requests: 1,
		},
		{
			name:     "paginated compare",
			repo:     repoURL,
			sha:      headSHA,
			handler:  compareHandler(150, long),
			found:    map[string][]model.BuildCommit{"PROJQUAY-140": in(141)},
			requests: 2,
		},
		{
			name:     "truncated compare",
			repo:     repoURL,
			sha:      headSHA,
			handler:  compareHandler(151, long),
			reason:   ReasonTruncated,
			requests: 2,
		},
		{
			name:     "api 5xx",
			repo:     repoURL,
			sha:      headSHA,
			handler:  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			reason:   ReasonAPIError,
			requests: 1,
			failed:   true,
		},
		{
			name:   "short sha",
			repo:   repoURL,
			sha:    "bbbbbbb",
			reason: ReasonNoProvenance,
		},
		{
			name:   "not on github.com",
			repo:   "https://gitlab.com/quay/quay",
			sha:    headSHA,
			reason: ReasonNotGitHub,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if tc.handler == nil {
					t.Errorf("unexpected request %s", r.URL)
					return
				}
				tc.handler(w, r)
			}))
			defer srv.Close()
			b := build(tc.repo, tc.sha)
			s := newScanner(srv.URL, srv.Client(), &fakeStore{build: b})
			reg := syncstatus.New()
			s.Status = reg.Track("github-evidence", 0)
			s.ScanOnce(context.Background())
			if failed := len(reg.Problems(time.Now())) > 0; failed != tc.failed {
				t.Errorf("pass failed = %v, want %v", failed, tc.failed)
			}
			if requests != tc.requests {
				t.Errorf("requests = %d, want %d", requests, tc.requests)
			}
			found, notCompared := s.Tickets(b)
			wantNotCompared := []model.NotCompared{}
			if tc.reason != "" {
				wantNotCompared = []model.NotCompared{{Component: "operator", Reason: tc.reason}, {Component: "operator-bundle", Reason: tc.reason}}
			}
			if !maps.EqualFunc(found, tc.found, slices.Equal) || !slices.Equal(notCompared, wantNotCompared) {
				t.Errorf("Tickets = %+v, %+v; want %+v, %+v", found, notCompared, tc.found, wantNotCompared)
			}

			// A second pass reuses every outcome; an API error waits out
			// retryAfter.
			requests = 0
			s.ScanOnce(context.Background())
			if requests != 0 {
				t.Errorf("second pass requests = %d, want 0", requests)
			}
		})
	}
}

// An API error is compared again once retryAfter has passed, and an outcome
// no selected build uses is forgotten.
func TestScanOnceRetryAndForget(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	store := &fakeStore{build: build(repoURL, headSHA)}
	s := newScanner(srv.URL, srv.Client(), store)
	s.ScanOnce(context.Background())
	for id, r := range s.results {
		r.checked = r.checked.Add(-retryAfter)
		s.results[id] = r
	}
	s.ScanOnce(context.Background())
	if requests != 2 {
		t.Errorf("requests = %d, want 2", requests)
	}

	store.build = nil
	s.ScanOnce(context.Background())
	if len(s.results) != 0 {
		t.Errorf("results = %+v, want none", s.results)
	}
}

func TestTicketsNotScanned(t *testing.T) {
	var s *Scanner
	found, notCompared := s.Tickets(build(repoURL, headSHA))
	want := []model.NotCompared{{Component: "operator", Reason: ReasonNotScanned}, {Component: "operator-bundle", Reason: ReasonNotScanned}}
	if len(found) != 0 || !slices.Equal(notCompared, want) {
		t.Errorf("nil Scanner Tickets = %+v, %+v; want none, %+v", found, notCompared, want)
	}
}
