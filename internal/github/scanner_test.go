package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	builds []*model.SelectedBuild
	scans  map[[3]string]Scan
}

func (f *fakeStore) ListAllReleaseVersions(context.Context) ([]model.ReleaseVersion, error) {
	return []model.ReleaseVersion{{Name: "quay-v3.18.1"}, {Name: "quay-v3.17.9", Released: true}}, nil
}

func (f *fakeStore) StageBuildHistory(_ context.Context, r *model.ReleaseVersion, _ *regexp.Regexp) ([]*model.SelectedBuild, error) {
	if r.Name != "quay-v3.18.1" {
		return nil, fmt.Errorf("scanned released version %s", r.Name)
	}
	return f.builds, nil
}

func (f *fakeStore) ListEvidenceScans(_ context.Context, snapshot string) ([]Scan, error) {
	var out []Scan
	for k, s := range f.scans {
		if k[0] == snapshot {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) CompleteScan(_ context.Context, repo, base, head string) (*Scan, error) {
	for _, s := range f.scans {
		if s.State == StateComplete && s.Repo == repo && s.BaseSHA == base && s.HeadSHA == head {
			return &s, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) StoreEvidenceScan(_ context.Context, s Scan) error {
	f.scans[[3]string{s.Snapshot, s.Component, s.ImageDigest}] = s
	return nil
}

const repoURL = "https://github.com/quay/quay"

var (
	baseSHA = strings.Repeat("a", 40)
	headSHA = strings.Repeat("b", 40)
)

func build(snapshot, repo, sha string) *model.SelectedBuild {
	return &model.SelectedBuild{SnapshotName: snapshot, Components: []model.SelectedBuildComponent{
		{Name: "quay", ImageDigest: "sha256:" + snapshot, UpstreamRepo: repo, UpstreamSHA: sha},
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

// compareHandler serves base...head as status with all, paginated, and
// answers every commit's pulls lookup with a merged PR named after it.
func compareHandler(status string, behind, total int, all []commitJSON) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulls") {
			sha := strings.Split(r.URL.Path, "/")[5]
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"html_url": "https://github.com/quay/quay/pull/open", "merged_at": nil},
				{"html_url": "https://github.com/quay/quay/pull/" + sha[38:], "merged_at": "2026-10-01T00:00:00Z"},
			})
			return
		}
		if r.URL.Path != "/repos/quay/quay/compare/"+baseSHA+"..."+headSHA {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		lo, hi := min((page-1)*per, len(all)), min(page*per, len(all))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status, "ahead_by": total, "behind_by": behind, "total_commits": total, "commits": all[lo:hi],
		})
	}
}

func TestScanOnce(t *testing.T) {
	linear := commits(3, func(i int) string {
		return []string{"PROJQUAY-101: fix\n\nAlso PROJQUAY-101 and PROJQUAY-7.", "NO-ISSUE: chore XPROJQUAY-9", "deps"}[i]
	})
	long := commits(150, func(i int) string {
		if i == 140 {
			return "PROJQUAY-140 late"
		}
		return "NO-ISSUE"
	})
	ev := func(key string, i int, pr string) Evidence {
		sha := fmt.Sprintf("%040x", i)
		return Evidence{Key: key, CommitSHA: sha, CommitURL: "https://github.com/quay/quay/commit/" + sha, PRURL: "https://github.com/quay/quay/pull/" + pr, Source: SourceCompare}
	}
	for _, tc := range []struct {
		name     string
		builds   []*model.SelectedBuild
		handler  http.HandlerFunc
		stored   []Scan
		state    string
		reason   string
		evidence []Evidence
		requests int
		failed   bool
	}{
		{
			name:     "linear range with keys",
			builds:   []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			handler:  compareHandler("ahead", 0, 3, linear),
			state:    StateComplete,
			evidence: []Evidence{ev("PROJQUAY-101", 1, "01"), ev("PROJQUAY-7", 1, "01")},
			requests: 2,
		},
		{
			name:     "paginated compare",
			builds:   []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			handler:  compareHandler("ahead", 0, 150, long),
			state:    StateComplete,
			evidence: []Evidence{ev("PROJQUAY-140", 141, "8d")},
			requests: 3,
		},
		{
			name:     "truncated compare",
			builds:   []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			handler:  compareHandler("ahead", 0, 151, long),
			state:    StateUnknown,
			reason:   ReasonTruncated,
			requests: 2,
		},
		{
			name:     "diverged",
			builds:   []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			handler:  compareHandler("diverged", 2, 3, linear),
			state:    StateUnknown,
			reason:   ReasonDiverged,
			requests: 1,
		},
		{
			name:   "repo changed",
			builds: []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", "https://github.com/quay/quay-operator", baseSHA)},
			state:  StateUnknown,
			reason: ReasonRepoChanged,
		},
		{
			name:   "no baseline",
			builds: []*model.SelectedBuild{build("new", repoURL, headSHA)},
			state:  StateUnknown,
			reason: ReasonNoBaseline,
		},
		{
			name:   "short sha",
			builds: []*model.SelectedBuild{build("new", repoURL, "bbbbbbb"), build("old", repoURL, baseSHA)},
			state:  StateUnknown,
			reason: ReasonNoProvenance,
		},
		{
			name:     "api 5xx",
			builds:   []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			handler:  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			state:    StateUnknown,
			reason:   ReasonAPIError,
			requests: 1,
			failed:   true,
		},
		{
			name:   "cache hit on another snapshot",
			builds: []*model.SelectedBuild{build("new", repoURL, headSHA), build("old", repoURL, baseSHA)},
			stored: []Scan{{Snapshot: "rebuilt", Component: "quay", ImageDigest: "sha256:rebuilt", Repo: repoURL, BaseSHA: baseSHA, HeadSHA: headSHA,
				State: StateComplete, Evidence: []Evidence{ev("PROJQUAY-5", 5, "5")}}},
			state:    StateComplete,
			evidence: []Evidence{ev("PROJQUAY-5", 5, "5")},
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
			store := &fakeStore{builds: tc.builds, scans: map[[3]string]Scan{}}
			for _, s := range tc.stored {
				store.scans[[3]string{s.Snapshot, s.Component, s.ImageDigest}] = s
			}
			s := NewScanner(NewClient(srv.URL, "", srv.Client()), store, regexp.MustCompile(`.`), slog.New(slog.NewTextHandler(io.Discard, nil)))
			reg := syncstatus.New()
			s.Status = reg.Track("github-evidence", 0)
			s.ScanOnce(context.Background())
			if failed := len(reg.Problems(time.Now())) > 0; failed != tc.failed {
				t.Errorf("pass failed = %v, want %v", failed, tc.failed)
			}
			got, ok := store.scans[[3]string{"new", "quay", "sha256:new"}]
			if !ok {
				t.Fatalf("no scan stored for new; scans = %+v", store.scans)
			}
			if got.State != tc.state || got.Reason != tc.reason || !slices.Equal(got.Evidence, tc.evidence) {
				t.Errorf("scan = %s %q %+v, want %s %q %+v", got.State, got.Reason, got.Evidence, tc.state, tc.reason, tc.evidence)
			}
			if requests != tc.requests {
				t.Errorf("requests = %d, want %d", requests, tc.requests)
			}

			// A second pass re-reads nothing: complete and immutable outcomes
			// stand, and an API error waits out retryAfter.
			requests = 0
			s.ScanOnce(context.Background())
			if requests != 0 {
				t.Errorf("second pass requests = %d, want 0", requests)
			}
		})
	}
}
