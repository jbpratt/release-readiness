package github

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	ReasonNoProvenance = "upstream repo or full commit sha unresolved"
	ReasonNotGitHub    = "upstream repo is not on github.com"
	ReasonNotScanned   = "not scanned yet"
	ReasonAPIError     = "github api error"
	ReasonTruncated    = "compare did not return every commit"

	// retryAfter spaces out compares that failed on an API error.
	retryAfter = time.Hour
)

var (
	fullSHA    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ticketKeys = regexp.MustCompile(`\bPROJQUAY-[0-9]+\b`)
)

// Store is the subset of the database layer the scanner needs.
type Store interface {
	ListAllReleaseVersions(ctx context.Context) ([]model.ReleaseVersion, error)
	// SelectedStageBuild returns release's selected STAGE build, or nil and
	// the reason there is none.
	SelectedStageBuild(ctx context.Context, release *model.ReleaseVersion, plans *regexp.Regexp) (*model.SelectedBuild, string, error)
}

// commitID is an upstream commit of a component image.
type commitID struct{ repo, sha string }

// compared is the outcome of comparing a commit against its repo's default
// branch. reason is empty when every commit was read.
type compared struct {
	reason   string
	checked  time.Time
	mentions []mention
}

// mention is a ticket key in the message of a compared commit.
type mention struct{ key, sha, url string }

// Scanner compares the upstream commit of each component of the active
// releases' selected STAGE builds against its repo's default branch, and
// holds the outcomes in memory so requests never wait on GitHub.
type Scanner struct {
	client  *Client
	store   Store
	plans   *regexp.Regexp
	logger  *slog.Logger
	mu      sync.RWMutex
	results map[commitID]compared
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewScanner(client *Client, store Store, plans *regexp.Regexp, logger *slog.Logger) *Scanner {
	return &Scanner{client: client, store: store, plans: plans, logger: logger, results: make(map[commitID]compared)}
}

// Run scans immediately and then every interval until ctx is cancelled.
func (s *Scanner) Run(ctx context.Context, interval time.Duration) {
	s.ScanOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.ScanOnce(ctx)
		}
	}
}

// ScanOnce compares each commit of the active releases' selected STAGE builds
// that has no outcome yet, or whose compare failed on an API error retryAfter
// ago, and forgets the commits no selected build uses.
func (s *Scanner) ScanOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(pass.Err()) }()
	releases, err := s.store.ListAllReleaseVersions(ctx)
	if err != nil {
		pass.Add(fmt.Errorf("list releases: %w", err))
		return
	}
	now := time.Now().UTC()
	used := make(map[commitID]bool)
	for i := range releases {
		r := &releases[i]
		if r.Released || r.Archived {
			continue
		}
		b, _, err := s.store.SelectedStageBuild(ctx, r, s.plans)
		if err != nil {
			pass.Add(fmt.Errorf("stage build %s: %w", r.Name, err))
			continue
		}
		if b == nil {
			continue
		}
		for _, c := range b.Components {
			id := commitID{c.UpstreamRepo, c.UpstreamSHA}
			owner, repo, reason := githubRepo(c)
			if reason != "" || used[id] {
				continue
			}
			used[id] = true
			s.mu.RLock()
			old, ok := s.results[id]
			s.mu.RUnlock()
			if ok && (old.reason != ReasonAPIError || now.Sub(old.checked) < retryAfter) {
				continue
			}
			res, err := s.compare(ctx, owner, repo, c.UpstreamSHA)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.logger.Warn("compare", "repo", c.UpstreamRepo, "sha", c.UpstreamSHA, "error", err)
				pass.Add(fmt.Errorf("compare %s %s: %w", c.UpstreamRepo, c.UpstreamSHA, err))
			}
			res.checked = now
			s.mu.Lock()
			s.results[id] = res
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	maps.DeleteFunc(s.results, func(id commitID, _ compared) bool { return !used[id] })
	s.mu.Unlock()
}

// compare reads the commits sha gained after leaving owner/repo's default
// branch, which GitHub resolves for the base HEAD. On an API error the
// outcome carries ReasonAPIError and the error is returned.
func (s *Scanner) compare(ctx context.Context, owner, repo, sha string) (compared, error) {
	cmp, err := s.client.Compare(ctx, owner, repo, "HEAD", sha)
	if err != nil {
		return compared{reason: ReasonAPIError}, err
	}
	if len(cmp.Commits) != cmp.TotalCommits {
		return compared{reason: ReasonTruncated}, nil
	}
	var res compared
	for _, c := range cmp.Commits {
		for _, k := range uniqueKeys(c.Message) {
			res.mentions = append(res.mentions, mention{key: k, sha: c.SHA, url: c.HTMLURL})
		}
	}
	return res, nil
}

// Tickets returns the ticket keys named in the commits b's components gained
// after leaving their repos' default branches, each with the components and
// commits naming it, and the components whose commits were not read, with the
// reason. A nil Scanner has compared nothing.
func (s *Scanner) Tickets(b *model.SelectedBuild) (map[string][]model.BuildCommit, []model.NotCompared) {
	var results map[commitID]compared
	if s != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		results = s.results
	}
	found := make(map[string][]model.BuildCommit)
	notCompared := []model.NotCompared{}
	for _, c := range b.Components {
		res, ok := results[commitID{c.UpstreamRepo, c.UpstreamSHA}]
		if !ok {
			res.reason = ReasonNotScanned
		}
		if _, _, reason := githubRepo(c); reason != "" {
			res.reason = reason
		}
		if res.reason != "" {
			notCompared = append(notCompared, model.NotCompared{Component: c.Name, Reason: res.reason})
			continue
		}
		for _, m := range res.mentions {
			found[m.key] = append(found[m.key], model.BuildCommit{Component: c.Name, CommitSHA: m.sha, CommitURL: m.url})
		}
	}
	return found, notCompared
}

// githubRepo returns the owner and name of c's upstream repo, or the reason
// c's commit cannot be compared.
func githubRepo(c model.SelectedBuildComponent) (owner, repo, reason string) {
	if c.UpstreamRepo == "" || !fullSHA.MatchString(c.UpstreamSHA) {
		return "", "", ReasonNoProvenance
	}
	if owner, repo = splitRepo(c.UpstreamRepo); owner == "" {
		return "", "", ReasonNotGitHub
	}
	return owner, repo, ""
}

func uniqueKeys(msg string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, k := range ticketKeys.FindAllString(msg, -1) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

// splitRepo returns the owner and name of a https://github.com/owner/name
// URL, or empty strings for any other repo.
func splitRepo(repoURL string) (owner, repo string) {
	u, err := url.Parse(repoURL)
	if err != nil || u.Host != "github.com" {
		return "", ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}
