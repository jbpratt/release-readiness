package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Commit is a source commit.
type Commit struct {
	SHA, Message, URL string
}

// CommitFetcher lists the commits between two commits of a branch.
type CommitFetcher interface {
	// Range returns the commits after base up to and including head, oldest
	// first. It fails unless head descends from base and is on branch.
	Range(ctx context.Context, repo, branch, base, head string) ([]Commit, error)
}

// GitHubFetcher reads commit ranges from the GitHub REST API and caches them
// in memory.
type GitHubFetcher struct {
	baseURL string
	http    *http.Client
	token   func() string

	mu    sync.Mutex
	cache map[string][]Commit
}

// NewGitHubFetcher returns a fetcher using hc (nil uses
// http.DefaultClient). A nil token, or one returning "", is unauthenticated.
func NewGitHubFetcher(hc *http.Client, token func() string) *GitHubFetcher {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &GitHubFetcher{baseURL: "https://api.github.com", http: hc, token: token, cache: map[string][]Commit{}}
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

type comparison struct {
	Status          string `json:"status"`
	BehindBy        int    `json:"behind_by"`
	TotalCommits    int    `json:"total_commits"`
	MergeBaseCommit struct {
		SHA string `json:"sha"`
	} `json:"merge_base_commit"`
	Commits []struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
		} `json:"commit"`
	} `json:"commits"`
}

// Range implements CommitFetcher for https://github.com/{owner}/{repo}.
func (f *GitHubFetcher) Range(ctx context.Context, repo, branch, base, head string) ([]Commit, error) {
	rest, ok := strings.CutPrefix(repo, "https://github.com/")
	parts := strings.Split(rest, "/")
	if !ok || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("unsupported host or repository %q", repo)
	}
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(head) {
		return nil, fmt.Errorf("base %q and head %q must be full commit SHAs", base, head)
	}
	if base == head {
		return []Commit{}, nil
	}
	// Branch is in the key so a range is never served for a branch it was
	// not checked against.
	key := strings.Join([]string{repo, branch, base, head}, " ")
	f.mu.Lock()
	cached, ok := f.cache[key]
	f.mu.Unlock()
	if ok {
		return slices.Clone(cached), nil
	}

	api := f.baseURL + "/repos/" + rest
	what := fmt.Sprintf("compare %s %s...%s", repo, base, head)
	var commits []Commit
	total := 0
	for next := api + "/compare/" + base + "..." + head + "?per_page=100&page=1"; next != ""; {
		// The token goes only to the API host, whatever a Link header names.
		if !strings.HasPrefix(next, f.baseURL+"/") {
			return nil, fmt.Errorf("%s: next page %q is off %s", what, next, f.baseURL)
		}
		var c comparison
		var err error
		if next, err = f.get(ctx, next, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		if c.MergeBaseCommit.SHA != base {
			return nil, fmt.Errorf("%s: merge base is %s, not base", what, c.MergeBaseCommit.SHA)
		}
		if c.BehindBy != 0 {
			return nil, fmt.Errorf("%s: diverged, head is %d commits behind base", what, c.BehindBy)
		}
		total = c.TotalCommits
		for _, cm := range c.Commits {
			commits = append(commits, Commit{SHA: cm.SHA, Message: cm.Commit.Message, URL: cm.HTMLURL})
		}
	}
	if len(commits) != total {
		return nil, fmt.Errorf("%s: got %d commits, total_commits is %d", what, len(commits), total)
	}
	if len(commits) > 0 && commits[len(commits)-1].SHA != head {
		return nil, fmt.Errorf("%s: last commit is %s, not head", what, commits[len(commits)-1].SHA)
	}

	var onBranch comparison
	if _, err := f.get(ctx, api+"/compare/"+head+"..."+branch, &onBranch); err != nil {
		return nil, fmt.Errorf("compare %s %s...%s: %w", repo, head, branch, err)
	}
	if onBranch.Status != "ahead" && onBranch.Status != "identical" {
		return nil, fmt.Errorf("%s: head is not on branch %s (status %q)", what, branch, onBranch.Status)
	}

	f.mu.Lock()
	f.cache[key] = commits
	f.mu.Unlock()
	return slices.Clone(commits), nil
}

// get decodes u's JSON into v and returns its rel="next" link, "" for none.
func (f *GitHubFetcher) get(ctx context.Context, u string, v any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if f.token != nil {
		if t := f.token(); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GET %s: %s", u, resp.Status)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				return "", fmt.Errorf("%w: rate limited, retry after %ss", err, ra)
			}
			if reset, perr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); perr == nil {
				return "", fmt.Errorf("%w: rate limited until %s", err, time.Unix(reset, 0).UTC().Format(time.RFC3339))
			}
		}
		return "", err
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return "", fmt.Errorf("decode %s: %w", u, err)
	}
	for link := range strings.SplitSeq(resp.Header.Get("Link"), ",") {
		target, params, _ := strings.Cut(link, ";")
		if strings.Contains(params, `rel="next"`) {
			return strings.Trim(target, " <>"), nil
		}
	}
	return "", nil
}
