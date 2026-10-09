package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	StateComplete = "complete"
	StateUnknown  = "unknown"

	ReasonNoProvenance       = "upstream repo or full commit sha unresolved"
	ReasonNoBaseline         = "no previous stage build of this component"
	ReasonBaselineUnresolved = "previous stage build upstream unresolved"
	ReasonRepoChanged        = "upstream repo changed since previous stage build"
	ReasonNotGitHub          = "upstream repo is not on github.com"
	ReasonDiverged           = "previous stage build is not an ancestor"
	ReasonTruncated          = "compare did not return every commit"
	ReasonAPIError           = "github api error"

	SourceCompare = "github-compare"

	// retryAfter spaces out compares that failed on an API error.
	retryAfter = time.Hour
)

var (
	fullSHA    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ticketKeys = regexp.MustCompile(`\bPROJQUAY-[0-9]+\b`)
)

// Scan is the compare of one STAGE build component image against the same
// component in the previous successful STAGE build. Evidence is empty unless
// State is StateComplete.
type Scan struct {
	Snapshot, Component, ImageDigest string
	Repo, BaseSHA, HeadSHA           string
	State, Reason                    string
	CheckedAt                        time.Time
	Evidence                         []Evidence
}

// Evidence is a ticket key in the message of a commit in a scan's range.
type Evidence struct {
	Key, CommitSHA, CommitURL, PRURL, Source string
}

// Store is the subset of the database layer the scanner needs.
type Store interface {
	ListAllReleaseVersions(ctx context.Context) ([]model.ReleaseVersion, error)
	// StageBuildHistory returns release's successful STAGE builds, newest first.
	StageBuildHistory(ctx context.Context, release *model.ReleaseVersion, plans *regexp.Regexp) ([]*model.SelectedBuild, error)
	// ListEvidenceScans returns snapshot's scans with their evidence.
	ListEvidenceScans(ctx context.Context, snapshot string) ([]Scan, error)
	// CompleteScan returns a complete scan of repo's base...head with its
	// evidence, from any Snapshot, or nil.
	CompleteScan(ctx context.Context, repo, base, head string) (*Scan, error)
	// StoreEvidenceScan upserts s and replaces its evidence.
	StoreEvidenceScan(ctx context.Context, s Scan) error
}

// Scanner compares each component of every successful STAGE build of the
// active releases against the build before it.
type Scanner struct {
	client *Client
	store  Store
	plans  *regexp.Regexp
	logger *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewScanner(client *Client, store Store, plans *regexp.Regexp, logger *slog.Logger) *Scanner {
	return &Scanner{client: client, store: store, plans: plans, logger: logger}
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

// ScanOnce scans every component whose stored scan is missing, covers a
// different range, or is unknown for a cause that can change.
func (s *Scanner) ScanOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(pass.Err()) }()
	releases, err := s.store.ListAllReleaseVersions(ctx)
	if err != nil {
		pass.Add(fmt.Errorf("list releases: %w", err))
		return
	}
	now := time.Now().UTC()
	for i := range releases {
		r := &releases[i]
		if r.Released || r.Archived {
			continue
		}
		builds, err := s.store.StageBuildHistory(ctx, r, s.plans)
		if err != nil {
			pass.Add(fmt.Errorf("stage builds %s: %w", r.Name, err))
			continue
		}
		for j, b := range builds {
			var prev *model.SelectedBuild
			if j+1 < len(builds) {
				prev = builds[j+1]
			}
			if err := s.scanBuild(ctx, b, prev, now, &pass); err != nil {
				if ctx.Err() != nil {
					return
				}
				pass.Add(fmt.Errorf("scan %s: %w", b.SnapshotName, err))
			}
		}
	}
}

func (s *Scanner) scanBuild(ctx context.Context, b, prev *model.SelectedBuild, now time.Time, pass *syncstatus.Pass) error {
	stored, err := s.store.ListEvidenceScans(ctx, b.SnapshotName)
	if err != nil {
		return err
	}
	old := make(map[[2]string]Scan, len(stored))
	for _, sc := range stored {
		old[[2]string{sc.Component, sc.ImageDigest}] = sc
	}
	for _, c := range b.Components {
		want, owner, repo := prepare(b, prev, c)
		if o, ok := old[[2]string{c.Name, c.ImageDigest}]; ok && o.Repo == want.Repo && o.BaseSHA == want.BaseSHA && o.HeadSHA == want.HeadSHA {
			if want.State != "" && o.State == want.State && o.Reason == want.Reason {
				continue
			}
			if want.State == "" && settled(o, now) {
				continue
			}
		}
		want.CheckedAt = now
		if want.State == "" {
			cached, err := s.store.CompleteScan(ctx, want.Repo, want.BaseSHA, want.HeadSHA)
			if err != nil {
				return err
			}
			if cached != nil {
				want.State, want.Evidence = StateComplete, cached.Evidence
			} else if err := s.compare(ctx, &want, owner, repo); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				s.logger.Warn("compare", "repo", want.Repo, "base", want.BaseSHA, "head", want.HeadSHA, "error", err)
				pass.Add(fmt.Errorf("compare %s %s...%s: %w", want.Repo, want.BaseSHA, want.HeadSHA, err))
			}
		}
		if err := s.store.StoreEvidenceScan(ctx, want); err != nil {
			return err
		}
	}
	return nil
}

// prepare fills a scan's range from the builds. Its State is empty when the
// range must be compared on GitHub as owner/repo, else the range decides it.
func prepare(b, prev *model.SelectedBuild, c model.SelectedBuildComponent) (sc Scan, owner, repo string) {
	sc = Scan{Snapshot: b.SnapshotName, Component: c.Name, ImageDigest: c.ImageDigest, Repo: c.UpstreamRepo, HeadSHA: c.UpstreamSHA}
	unknown := func(reason string) (Scan, string, string) {
		sc.State, sc.Reason = StateUnknown, reason
		return sc, "", ""
	}
	if c.UpstreamRepo == "" || !fullSHA.MatchString(c.UpstreamSHA) {
		return unknown(ReasonNoProvenance)
	}
	var base *model.SelectedBuildComponent
	if prev != nil {
		for i := range prev.Components {
			if prev.Components[i].Name == c.Name {
				base = &prev.Components[i]
			}
		}
	}
	if base == nil {
		return unknown(ReasonNoBaseline)
	}
	sc.BaseSHA = base.UpstreamSHA
	if base.UpstreamRepo == "" || !fullSHA.MatchString(base.UpstreamSHA) {
		return unknown(ReasonBaselineUnresolved)
	}
	if base.UpstreamRepo != c.UpstreamRepo {
		return unknown(ReasonRepoChanged)
	}
	if base.UpstreamSHA == c.UpstreamSHA {
		sc.State = StateComplete
		return sc, "", ""
	}
	if owner, repo = splitRepo(c.UpstreamRepo); owner == "" {
		return unknown(ReasonNotGitHub)
	}
	return sc, owner, repo
}

// settled reports whether a compared scan stands: its range is immutable, so
// only an API error is retried.
func settled(o Scan, now time.Time) bool {
	return o.Reason != ReasonAPIError || now.Sub(o.CheckedAt) < retryAfter
}

// compare decides sc from GitHub. On an API error sc is unknown with
// ReasonAPIError and the error is returned.
func (s *Scanner) compare(ctx context.Context, sc *Scan, owner, repo string) error {
	sc.State, sc.Reason = StateUnknown, ReasonAPIError
	cmp, err := s.client.Compare(ctx, owner, repo, sc.BaseSHA, sc.HeadSHA)
	if err != nil {
		return err
	}
	if (cmp.Status != "ahead" && cmp.Status != "identical") || cmp.BehindBy != 0 {
		sc.Reason = ReasonDiverged
		return nil
	}
	if len(cmp.Commits) != cmp.TotalCommits {
		sc.Reason = ReasonTruncated
		return nil
	}
	var evidence []Evidence
	for _, c := range cmp.Commits {
		keys := uniqueKeys(c.Message)
		if len(keys) == 0 {
			continue
		}
		pr, err := s.client.MergedPR(ctx, owner, repo, c.SHA)
		if err != nil {
			return err
		}
		for _, k := range keys {
			evidence = append(evidence, Evidence{Key: k, CommitSHA: c.SHA, CommitURL: c.HTMLURL, PRURL: pr, Source: SourceCompare})
		}
	}
	sc.State, sc.Reason, sc.Evidence = StateComplete, "", evidence
	return nil
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
