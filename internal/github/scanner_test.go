package github

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const repoURL = "https://github.com/quay/quay"

var (
	headSHA  = strings.Repeat("b", 40)
	baseSHA  = strings.Repeat("a", 40)
	clairSHA = strings.Repeat("c", 40)
)

// build is a candidate of two components from one commit, as an operator and
// its bundle are.
func build(repo, sha string) *model.TicketBuild {
	return &model.TicketBuild{Snapshot: "snap", Components: []model.TicketComponent{
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

// commit is commit n of quay/<repo>.
func commit(repo string, n int, msg string) commitJSON {
	c := commitJSON{SHA: fmt.Sprintf("%040x", n)}
	c.HTMLURL = "https://github.com/quay/" + repo + "/commit/" + c.SHA
	c.Commit.Message = msg
	return c
}

func commits(n int, msg func(i int) string) []commitJSON {
	out := make([]commitJSON, n)
	for i := range out {
		out[i] = commit("quay", i+1, msg(i))
	}
	return out
}

// reply is GitHub's answer to a compare: an HTTP error code, or its status and
// commits, of which there are total when set.
type reply struct {
	code    int
	status  string
	total   int
	commits []commitJSON
}

// fakeGitHub serves each compare, keyed "<repo> <base>...<head>", from replies,
// paginated, and any other as not found.
type fakeGitHub struct {
	mu       sync.Mutex
	replies  map[string]reply
	requests []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	repo, span, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/repos/quay/"), "/compare/")
	key := repo + " " + span
	f.requests = append(f.requests, key)
	c, ok := f.replies[key]
	if !ok || c.code != 0 {
		w.WriteHeader(cmp.Or(c.code, http.StatusNotFound))
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	lo, hi := min((page-1)*per, len(c.commits)), min(page*per, len(c.commits))
	total := c.total
	if total == 0 {
		total = len(c.commits)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": c.status, "total_commits": total, "commits": c.commits[lo:hi]})
}

func (f *fakeGitHub) set(key string, r reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[key] = r
}

// take returns the compares requested since the last take.
func (f *fakeGitHub) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.requests
	f.requests = nil
	return r
}

// newScanner scans builds through gh.
func newScanner(t *testing.T, gh *fakeGitHub, builds ...*model.TicketBuild) *Scanner {
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	return NewScanner(NewClient(srv.URL, "", srv.Client()), func(context.Context) ([]model.TicketBuild, error) {
		out := make([]model.TicketBuild, len(builds))
		for i, b := range builds {
			out[i] = *b
		}
		return out, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
		reply    *reply
		found    map[string][]model.BuildCommit
		reason   string
		requests int
		failed   bool
	}{
		{
			name:     "keys in commit messages",
			repo:     repoURL,
			sha:      headSHA,
			reply:    &reply{status: "diverged", commits: linear},
			found:    map[string][]model.BuildCommit{"PROJQUAY-101": in(1), "PROJQUAY-7": in(1, 3)},
			requests: 1,
		},
		{
			name:     "paginated compare",
			repo:     repoURL,
			sha:      headSHA,
			reply:    &reply{status: "diverged", commits: long},
			found:    map[string][]model.BuildCommit{"PROJQUAY-140": in(141)},
			requests: 2,
		},
		{
			name:     "truncated compare",
			repo:     repoURL,
			sha:      headSHA,
			reply:    &reply{status: "diverged", commits: long, total: 151},
			reason:   ReasonTruncated,
			requests: 2,
		},
		{
			name:     "api 5xx",
			repo:     repoURL,
			sha:      headSHA,
			reply:    &reply{code: http.StatusBadGateway},
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
			gh := &fakeGitHub{replies: map[string]reply{}}
			if tc.reply != nil {
				gh.set("quay HEAD..."+tc.sha, *tc.reply)
			}
			b := build(tc.repo, tc.sha)
			s := newScanner(t, gh, b)
			reg := syncstatus.New()
			s.Status = reg.Track("github-evidence", 0)
			s.ScanOnce(context.Background())
			if failed := len(reg.Problems(time.Now())) > 0; failed != tc.failed {
				t.Errorf("pass failed = %v, want %v", failed, tc.failed)
			}
			if requests := gh.take(); len(requests) != tc.requests {
				t.Errorf("requests = %v, want %d", requests, tc.requests)
			}
			e := s.Tickets(b)
			wantNotCompared := []model.NotCompared{}
			if tc.reason != "" {
				wantNotCompared = []model.NotCompared{{Component: "operator", Reason: tc.reason}, {Component: "operator-bundle", Reason: tc.reason}}
			}
			if !maps.EqualFunc(e.InBuild, tc.found, slices.Equal) || !slices.Equal(e.NotCompared, wantNotCompared) {
				t.Errorf("Tickets = %+v, %+v; want %+v, %+v", e.InBuild, e.NotCompared, tc.found, wantNotCompared)
			}

			// A second pass reuses every outcome; an API error waits out
			// retryAfter.
			s.ScanOnce(context.Background())
			if requests := gh.take(); len(requests) != 0 {
				t.Errorf("second pass requests = %v, want none", requests)
			}
		})
	}
}

// An API error is compared again once retryAfter has passed, and an outcome
// no build uses is forgotten.
func TestScanOnceRetryAndForget(t *testing.T) {
	gh := &fakeGitHub{replies: map[string]reply{}}
	s := newScanner(t, gh, build(repoURL, headSHA))
	t0 := time.Now()
	s.now = func() time.Time { return t0 }
	s.ScanOnce(context.Background())
	s.now = func() time.Time { return t0.Add(retryAfter) }
	s.ScanOnce(context.Background())
	if requests := gh.take(); len(requests) != 2 {
		t.Errorf("requests = %v, want 2", requests)
	}

	s.builds = func(context.Context) ([]model.TicketBuild, error) { return nil, nil }
	s.ScanOnce(context.Background())
	if len(s.results) != 0 {
		t.Errorf("results = %+v, want none", s.results)
	}
}

func TestTicketsNotScanned(t *testing.T) {
	var s *Scanner
	e := s.Tickets(build(repoURL, headSHA))
	want := []model.NotCompared{{Component: "operator", Reason: ReasonNotScanned}, {Component: "operator-bundle", Reason: ReasonNotScanned}}
	if len(e.InBuild) != 0 || !slices.Equal(e.NotCompared, want) {
		t.Errorf("nil Scanner Tickets = %+v, %+v; want none, %+v", e.InBuild, e.NotCompared, want)
	}
}

// A key no candidate commit names is not in the build when a default-branch
// commit the candidate lacks names it, and only when every component's own
// commits and that default-branch side were read completely.
func TestTicketsNotInBuild(t *testing.T) {
	quayFwd, clairFwd := "quay HEAD..."+headSHA, "clair HEAD..."+clairSHA
	quayRev, clairRev := "quay "+headSHA+"...HEAD", "clair "+clairSHA+"...HEAD"
	missing := []commitJSON{commit("quay", 3, "PROJQUAY-3: fix"), commit("quay", 4, "PROJQUAY-1: follow-up")}
	replies := map[string]reply{
		quayFwd:  {status: "diverged", commits: []commitJSON{commit("quay", 1, "PROJQUAY-1: fix")}},
		clairFwd: {status: "ahead", commits: []commitJSON{commit("clair", 2, "PROJQUAY-2: fix")}},
		quayRev:  {status: "diverged", commits: missing},
		clairRev: {status: "behind"},
	}
	sha3 := fmt.Sprintf("%040x", 3)
	// PROJQUAY-1 is in the build, though the default branch has a commit
	// naming it the build lacks.
	red := map[string][]model.BuildCommit{"PROJQUAY-3": {{Component: "quay", CommitSHA: sha3, CommitURL: "https://github.com/quay/quay/commit/" + sha3}}}
	for _, tc := range []struct {
		name        string
		noTargets   bool
		change      map[string]reply
		red         map[string][]model.BuildCommit
		notCompared []model.NotCompared
		requests    int
	}{
		{name: "both sides read", red: red, requests: 4},
		{
			name:        "another component's commits not read",
			change:      map[string]reply{clairFwd: {code: http.StatusBadGateway}},
			notCompared: []model.NotCompared{{Component: "clair", Reason: ReasonAPIError}},
			requests:    4,
		},
		{
			name:        "default branch read failed",
			change:      map[string]reply{quayRev: {code: http.StatusBadGateway}},
			notCompared: []model.NotCompared{{Component: "quay", Reason: defaultBranch + ReasonAPIError}},
			requests:    4,
		},
		{
			name:        "default branch read truncated",
			change:      map[string]reply{quayRev: {status: "diverged", commits: missing, total: 3}},
			notCompared: []model.NotCompared{{Component: "quay", Reason: defaultBranch + ReasonTruncated}},
			requests:    4,
		},
		{name: "no Target Version tickets", noTargets: true, requests: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{replies: maps.Clone(replies)}
			maps.Copy(gh.replies, tc.change)
			b := &model.TicketBuild{Targets: !tc.noTargets, Components: []model.TicketComponent{
				{Name: "quay", UpstreamRepo: repoURL, UpstreamSHA: headSHA},
				{Name: "clair", UpstreamRepo: "https://github.com/quay/clair", UpstreamSHA: clairSHA},
			}}
			s := newScanner(t, gh, b)
			s.ScanOnce(context.Background())
			if requests := gh.take(); len(requests) != tc.requests {
				t.Errorf("requests = %v, want %d", requests, tc.requests)
			}
			e := s.Tickets(b)
			if tc.red == nil {
				tc.red = map[string][]model.BuildCommit{}
			}
			if tc.notCompared == nil {
				tc.notCompared = []model.NotCompared{}
			}
			if !maps.EqualFunc(e.NotInBuild, tc.red, slices.Equal) || !slices.Equal(e.NotCompared, tc.notCompared) {
				t.Errorf("not in build %+v, not compared %+v; want %+v, %+v", e.NotInBuild, e.NotCompared, tc.red, tc.notCompared)
			}
		})
	}
}

// What the default branch has that a build lacks is read again an hour after
// its last read, unlike the build's own commits.
func TestScanOnceRereadsDefaultBranch(t *testing.T) {
	rev := "quay " + headSHA + "...HEAD"
	gh := &fakeGitHub{replies: map[string]reply{"quay HEAD..." + headSHA: {status: "diverged"}, rev: {status: "diverged"}}}
	b := build(repoURL, headSHA)
	b.Targets = true
	s := newScanner(t, gh, b)
	t0 := time.Now()
	s.now = func() time.Time { return t0 }
	s.ScanOnce(context.Background())
	if requests := gh.take(); len(requests) != 2 {
		t.Fatalf("first pass requests = %v, want both sides", requests)
	}

	gh.set(rev, reply{status: "diverged", commits: []commitJSON{commit("quay", 9, "PROJQUAY-9: fix")}})
	for _, step := range []struct {
		after    time.Duration
		requests []string
		red      bool
	}{
		{10 * time.Minute, nil, false},
		{retryAfter - time.Second, nil, false},
		{retryAfter, []string{rev}, true},
		{retryAfter + 10*time.Minute, nil, true},
	} {
		s.now = func() time.Time { return t0.Add(step.after) }
		s.ScanOnce(context.Background())
		if requests := gh.take(); !slices.Equal(requests, step.requests) {
			t.Errorf("after %v: requests = %v, want %v", step.after, requests, step.requests)
		}
		if _, red := s.Tickets(b).NotInBuild["PROJQUAY-9"]; red != step.red {
			t.Errorf("after %v: PROJQUAY-9 not in build = %v, want %v", step.after, red, step.red)
		}
	}
}

// A component's .z keys are those its commits since its base name, when its
// base is that commit or an ancestor of it; otherwise those its own commits
// name.
func TestTicketsZ(t *testing.T) {
	own := []commitJSON{commit("quay", 1, "PROJQUAY-1: fix"), commit("quay", 2, "PROJQUAY-2: fix")}
	since := "quay " + baseSHA + "..." + headSHA
	for _, tc := range []struct {
		name     string
		base     string
		reply    *reply
		z        []string
		zSince   bool
		requests int
	}{
		{name: "same commit", base: headSHA, zSince: true, requests: 1},
		{name: "identical", base: baseSHA, reply: &reply{status: "identical"}, zSince: true, requests: 2},
		{
			name: "ahead", base: baseSHA, reply: &reply{status: "ahead", commits: own[1:]},
			z: []string{"PROJQUAY-2"}, zSince: true, requests: 2,
		},
		{name: "behind", base: baseSHA, reply: &reply{status: "behind"}, z: []string{"PROJQUAY-1", "PROJQUAY-2"}, requests: 2},
		{name: "diverged", base: baseSHA, reply: &reply{status: "diverged", commits: own[1:]}, z: []string{"PROJQUAY-1", "PROJQUAY-2"}, requests: 2},
		{name: "compare failed", base: baseSHA, reply: &reply{code: http.StatusBadGateway}, z: []string{"PROJQUAY-1", "PROJQUAY-2"}, requests: 2},
		{name: "no base", z: []string{"PROJQUAY-1", "PROJQUAY-2"}, requests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{replies: map[string]reply{"quay HEAD..." + headSHA: {status: "diverged", commits: own}}}
			if tc.reply != nil {
				gh.set(since, *tc.reply)
			}
			b := &model.TicketBuild{Components: []model.TicketComponent{{Name: "quay", UpstreamRepo: repoURL, UpstreamSHA: headSHA, BaseSHA: tc.base}}}
			s := newScanner(t, gh, b)
			s.ScanOnce(context.Background())
			if requests := gh.take(); len(requests) != tc.requests {
				t.Errorf("requests = %v, want %d", requests, tc.requests)
			}
			e := s.Tickets(b)
			if z := slices.Sorted(maps.Keys(e.Z)); !slices.Equal(z, tc.z) || e.ZSince != tc.zSince {
				t.Errorf("z = %v, since %v; want %v, %v", z, e.ZSince, tc.z, tc.zSince)
			}
		})
	}
}
