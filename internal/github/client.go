// Package github proves which tickets a STAGE build carries by reading the
// upstream commits between it and the previous STAGE build through the
// read-only GitHub REST API.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const perPage = 100

// Client reads the GitHub REST API. It only issues GETs.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a client for baseURL; an empty token reads anonymously.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// Commit is one commit of a compared range.
type Commit struct {
	SHA     string
	HTMLURL string
	Message string
}

// Comparison is base...head with every page of its commits read.
type Comparison struct {
	Status       string
	AheadBy      int
	BehindBy     int
	TotalCommits int
	Commits      []Commit
}

// Compare reads owner/repo's base...head, following pages until all
// TotalCommits are read or a page comes back short.
func (c *Client) Compare(ctx context.Context, owner, repo, base, head string) (*Comparison, error) {
	var cmp Comparison
	for page := 1; ; page++ {
		var resp struct {
			Status       string `json:"status"`
			AheadBy      int    `json:"ahead_by"`
			BehindBy     int    `json:"behind_by"`
			TotalCommits int    `json:"total_commits"`
			Commits      []struct {
				SHA     string `json:"sha"`
				HTMLURL string `json:"html_url"`
				Commit  struct {
					Message string `json:"message"`
				} `json:"commit"`
			} `json:"commits"`
		}
		path := fmt.Sprintf("/repos/%s/%s/compare/%s...%s?per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), base, head, perPage, page)
		if err := c.get(ctx, path, &resp); err != nil {
			return nil, err
		}
		if page == 1 {
			cmp.Status, cmp.AheadBy, cmp.BehindBy, cmp.TotalCommits = resp.Status, resp.AheadBy, resp.BehindBy, resp.TotalCommits
		}
		for _, rc := range resp.Commits {
			cmp.Commits = append(cmp.Commits, Commit{SHA: rc.SHA, HTMLURL: rc.HTMLURL, Message: rc.Commit.Message})
		}
		if len(cmp.Commits) >= cmp.TotalCommits || len(resp.Commits) < perPage {
			return &cmp, nil
		}
	}
}

// MergedPR returns the URL of the first merged pull request GitHub associates
// with commit sha, or "" when there is none.
func (c *Client) MergedPR(ctx context.Context, owner, repo, sha string) (string, error) {
	var prs []struct {
		HTMLURL  string  `json:"html_url"`
		MergedAt *string `json:"merged_at"`
	}
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s/pulls", url.PathEscape(owner), url.PathEscape(repo), sha), &prs); err != nil {
		return "", err
	}
	for _, pr := range prs {
		if pr.MergedAt != nil {
			return pr.HTMLURL, nil
		}
	}
	return "", nil
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
