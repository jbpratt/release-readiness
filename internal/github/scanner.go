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

	// defaultBranch prefixes the reason the default branch's commits a
	// component lacks were not read.
	defaultBranch = "default branch: "

	// retryAfter spaces out compares that failed on an API error, and reads
	// of the commits a default branch, which moves, has that a build lacks.
	retryAfter = time.Hour
)

var (
	fullSHA    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ticketKeys = regexp.MustCompile(`\bPROJQUAY-[0-9]+\b`)
)

// span is a repo's base...head, where HEAD is its default branch.
type span struct{ repo, base, head string }

// compared is the outcome of a compare. reason is empty, and status set, when
// every commit was read.
type compared struct {
	reason   string
	status   string
	checked  time.Time
	mentions []mention
}

// mention is a ticket key in the message of a compared commit.
type mention struct{ key, sha, url string }

// Scanner compares the upstream commit of each component of the candidate
// builds builds returns with its repo's default branch and with its base, and
// holds the outcomes in memory so requests never wait on GitHub.
type Scanner struct {
	client  *Client
	builds  func(context.Context) ([]model.TicketBuild, error)
	logger  *slog.Logger
	now     func() time.Time
	mu      sync.RWMutex
	results map[span]compared
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewScanner(client *Client, builds func(context.Context) ([]model.TicketBuild, error), logger *slog.Logger) *Scanner {
	return &Scanner{client: client, builds: builds, logger: logger, now: time.Now, results: make(map[span]compared)}
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

// ScanOnce compares each component of the builds: HEAD...sha, the commits it
// gained after leaving its repo's default branch; base...sha, those since its
// base, unless that is sha; and when its build has Target Version tickets,
// sha...HEAD, the default branch's commits it lacks. A compare is read again
// only after an API error, retryAfter later, or for sha...HEAD, retryAfter
// after its last read. It forgets the compares no build uses.
func (s *Scanner) ScanOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(pass.Err()) }()
	builds, err := s.builds(ctx)
	if err != nil {
		pass.Add(fmt.Errorf("list builds: %w", err))
		return
	}
	now := s.now().UTC()
	used := make(map[span]bool)
	for _, b := range builds {
		for _, c := range b.Components {
			owner, repo, reason := githubRepo(c)
			if reason != "" {
				continue
			}
			spans := []span{{c.UpstreamRepo, "HEAD", c.UpstreamSHA}}
			if fullSHA.MatchString(c.BaseSHA) && c.BaseSHA != c.UpstreamSHA {
				spans = append(spans, span{c.UpstreamRepo, c.BaseSHA, c.UpstreamSHA})
			}
			if b.Targets {
				spans = append(spans, span{c.UpstreamRepo, c.UpstreamSHA, "HEAD"})
			}
			for _, id := range spans {
				if used[id] {
					continue
				}
				used[id] = true
				s.mu.RLock()
				old, ok := s.results[id]
				s.mu.RUnlock()
				if ok && (now.Sub(old.checked) < retryAfter || old.reason != ReasonAPIError && id.head != "HEAD") {
					continue
				}
				res, err := s.compare(ctx, owner, repo, id.base, id.head)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					s.logger.Warn("compare", "repo", c.UpstreamRepo, "base", id.base, "head", id.head, "error", err)
					pass.Add(fmt.Errorf("compare %s %s...%s: %w", c.UpstreamRepo, id.base, id.head, err))
				}
				res.checked = now
				s.mu.Lock()
				s.results[id] = res
				s.mu.Unlock()
			}
		}
	}
	s.mu.Lock()
	maps.DeleteFunc(s.results, func(id span, _ compared) bool { return !used[id] })
	s.mu.Unlock()
}

// compare reads owner/repo's base...head. On an API error the outcome carries
// ReasonAPIError and the error is returned.
func (s *Scanner) compare(ctx context.Context, owner, repo, base, head string) (compared, error) {
	cmp, err := s.client.Compare(ctx, owner, repo, base, head)
	if err != nil {
		return compared{reason: ReasonAPIError}, err
	}
	if len(cmp.Commits) != cmp.TotalCommits {
		return compared{reason: ReasonTruncated}, nil
	}
	res := compared{status: cmp.Status}
	for _, c := range cmp.Commits {
		for _, k := range uniqueKeys(c.Message) {
			res.mentions = append(res.mentions, mention{key: k, sha: c.SHA, url: c.HTMLURL})
		}
	}
	return res, nil
}

// Evidence is what the compares of a build's components say about its
// tickets. InBuild holds, per ticket key, the commits naming it that the
// components gained after leaving their repos' default branches. Z holds the
// keys of the build's .z tickets: those its components' commits since their
// base name, or for a component whose base is unset, behind, diverged or not
// read, its InBuild commits; ZSince is set when a base was used. NotInBuild
// holds, per key no InBuild commit names, the default branches' commits the
// components lack that name it, and nothing unless every component's InBuild
// commits were read. NotCompared lists the components whose commits were not
// read, with the reason.
type Evidence struct {
	InBuild, NotInBuild map[string][]model.BuildCommit
	Z                   map[string]bool
	ZSince              bool
	NotCompared         []model.NotCompared
}

// Tickets returns what the compares of b's components say about its tickets.
// A nil Scanner has compared nothing.
func (s *Scanner) Tickets(b *model.TicketBuild) Evidence {
	var results map[span]compared
	if s != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		results = s.results
	}
	read := func(id span) compared {
		res, ok := results[id]
		if !ok {
			res.reason = ReasonNotScanned
		}
		return res
	}
	e := Evidence{InBuild: map[string][]model.BuildCommit{}, NotInBuild: map[string][]model.BuildCommit{}, Z: map[string]bool{}, NotCompared: []model.NotCompared{}}
	missing := map[string][]model.BuildCommit{}
	complete := true
	for _, c := range b.Components {
		if _, _, reason := githubRepo(c); reason != "" {
			e.NotCompared = append(e.NotCompared, model.NotCompared{Component: c.Name, Reason: reason})
			complete = false
			continue
		}
		add := func(found map[string][]model.BuildCommit, res compared) {
			for _, m := range res.mentions {
				found[m.key] = append(found[m.key], model.BuildCommit{Component: c.Name, CommitSHA: m.sha, CommitURL: m.url})
			}
		}
		fwd := read(span{c.UpstreamRepo, "HEAD", c.UpstreamSHA})
		if fwd.reason != "" {
			e.NotCompared = append(e.NotCompared, model.NotCompared{Component: c.Name, Reason: fwd.reason})
			complete = false
		}
		add(e.InBuild, fwd)
		// Only a complete read has a status.
		z := fwd
		if c.BaseSHA == c.UpstreamSHA {
			z, e.ZSince = compared{}, true
		} else if res := read(span{c.UpstreamRepo, c.BaseSHA, c.UpstreamSHA}); res.status == "ahead" || res.status == "identical" {
			z, e.ZSince = res, true
		}
		for _, m := range z.mentions {
			e.Z[m.key] = true
		}
		if b.Targets {
			rev := read(span{c.UpstreamRepo, c.UpstreamSHA, "HEAD"})
			if rev.reason != "" {
				e.NotCompared = append(e.NotCompared, model.NotCompared{Component: c.Name, Reason: defaultBranch + rev.reason})
			}
			add(missing, rev)
		}
	}
	if complete {
		for k, commits := range missing {
			if _, ok := e.InBuild[k]; !ok {
				e.NotInBuild[k] = commits
			}
		}
	}
	return e
}

// githubRepo returns the owner and name of c's upstream repo, or the reason
// c's commit cannot be compared.
func githubRepo(c model.TicketComponent) (owner, repo, reason string) {
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
