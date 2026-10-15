// Package jira provides a client for querying JIRA Cloud REST APIs.
package jira

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds JIRA connection settings.
type Config struct {
	BaseURL string // REST API base, e.g. https://redhat.atlassian.net
	SiteURL string // browse-link base; defaults to BaseURL
	Email   string // JIRA Cloud account email for Basic Auth
	Token   string // JIRA Cloud API token
	Project string // e.g. PROJQUAY
}

// Client is a JIRA REST API client.
type Client struct {
	baseURL    string
	siteURL    string
	email      string
	token      string
	project    string
	httpClient *http.Client
	minDelay   time.Duration // minimum delay between requests
}

// New creates a new JIRA client.
func New(cfg Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		siteURL: strings.TrimRight(cmp.Or(cfg.SiteURL, cfg.BaseURL), "/"),
		email:   cfg.Email,
		token:   cfg.Token,
		project: cfg.Project,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		minDelay: 1 * time.Second,
	}
}

// Issue represents a JIRA issue from the REST API.
type Issue struct {
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

// IssueFields holds the fields we care about from a JIRA issue.
type IssueFields struct {
	Summary   string     `json:"summary"`
	Assignee  *UserField `json:"assignee"`
	IssueType TypeField  `json:"issuetype"`
	Status    TypeField  `json:"status"`
	DueDate   string     `json:"duedate"`
}

type VersionField struct {
	Name        string `json:"name"`
	ReleaseDate string `json:"releaseDate"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
}

type UserField struct {
	DisplayName string `json:"displayName"`
}

type TypeField struct {
	Name string `json:"name"`
}

type searchResponse struct {
	NextPageToken string  `json:"nextPageToken,omitempty"`
	Issues        []Issue `json:"issues"`
}

// ActiveRelease represents a release discovered from JIRA via the -area/release component.
type ActiveRelease struct {
	FixVersion         string     // e.g. "quay-v3.16.3"
	DueDate            *time.Time // from the release ticket's dueDate field
	ReleaseTicketKey   string     // e.g. "PROJQUAY-10276"
	Assignee           string     // display name of the release ticket assignee
	KonfluxApplication string     // e.g. "quay-3-16" (derived from fixVersion)
}

// SiteURL returns the JIRA site URL used for browse links.
func (c *Client) SiteURL() string {
	return c.siteURL
}

// versionRe matches a known product followed by a version in release ticket summaries.
// A version without a known product, e.g. "fix was not included in 3.17.3", does not match.
// Examples:
//   - "Release Quay v3.16.2"       → product="quay", version="3.16.2"
//   - "Release OMR v2.0.10"        → product="omr", version="2.0.10"
//   - "⦗konflux⦘ Quay v3.15.3"    → product="quay", version="3.15.3"
var versionRe = regexp.MustCompile(`(?i)\b(quay|omr)\s+v?(\d+\.\d+(?:\.\d+)?)`)

// ParseVersionFromSummary extracts the product and version from a release ticket summary.
// Returns product (lowercased), version string, and whether a match was found.
func ParseVersionFromSummary(summary string) (product, version string, ok bool) {
	m := versionRe.FindStringSubmatch(summary)
	if m == nil {
		return "", "", false
	}
	product = strings.ToLower(m[1])
	version = m[2]
	return product, version, true
}

// DiscoverActiveReleases queries JIRA for active release tickets using the -area/release component.
// Returns releases that are not Closed/Done, each with their fixVersion (parsed from
// the ticket summary), dueDate, and ticket key.
func (c *Client) DiscoverActiveReleases(ctx context.Context) ([]ActiveRelease, error) {
	jql := fmt.Sprintf(
		`project=%s AND component="-area/release" AND status NOT IN (Closed, Done)`,
		c.project,
	)
	allIssues, err := c.search(ctx, jql, "summary,duedate,assignee", "discover releases")
	if err != nil {
		return nil, err
	}

	var releases []ActiveRelease
	for _, issue := range allIssues {
		product, version, ok := ParseVersionFromSummary(issue.Fields.Summary)
		if !ok {
			continue
		}

		// JIRA fixVersions always use "{product}-v{version}" format (e.g. "quay-v3.16.2", "omr-v2.0.10")
		fixVersion := product + "-v" + version

		assignee := ""
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.DisplayName
		}

		rel := ActiveRelease{
			FixVersion:         fixVersion,
			ReleaseTicketKey:   issue.Key,
			Assignee:           assignee,
			KonfluxApplication: FixVersionToKonfluxApp(fixVersion),
		}

		if t, err := time.Parse(time.DateOnly, issue.Fields.DueDate); err == nil {
			rel.DueDate = &t
		}

		releases = append(releases, rel)
	}

	return releases, nil
}

// SearchIssues queries JIRA for issues matching a Target Version.
// It handles pagination automatically and respects rate limits.
func (c *Client) SearchIssues(ctx context.Context, fixVersion string) ([]Issue, error) {
	jql := fmt.Sprintf(`project=%s AND "Target Version"="%s"`, c.project, fixVersion)
	return c.search(ctx, jql, "summary,issuetype,status", "search issues")
}

// search returns every issue jql matches, with fields, page by page. A
// failed request's error is prefixed with what.
func (c *Client) search(ctx context.Context, jql, fields, what string) ([]Issue, error) {
	var issues []Issue
	params := url.Values{"jql": {jql}, "fields": {fields}, "maxResults": {"100"}}
	for {
		body, err := c.doGetWithRetry(ctx, c.baseURL+"/rest/api/3/search/jql?"+params.Encode())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		var resp searchResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode search response: %w", err)
		}
		issues = append(issues, resp.Issues...)
		if resp.NextPageToken == "" {
			return issues, nil
		}
		params.Set("nextPageToken", resp.NextPageToken)
	}
}

// ListVersions fetches the project's versions from JIRA, keyed by name.
func (c *Client) ListVersions(ctx context.Context) (map[string]VersionField, error) {
	reqURL := fmt.Sprintf("%s/rest/api/3/project/%s/versions", c.baseURL, url.PathEscape(c.project))
	body, err := c.doGetWithRetry(ctx, reqURL)
	if err != nil {
		return nil, fmt.Errorf("get versions: %w", err)
	}

	var versions []VersionField
	if err := json.Unmarshal(body, &versions); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}

	byName := make(map[string]VersionField, len(versions))
	for _, v := range versions {
		byName[v.Name] = v
	}
	return byName, nil
}

// doGetWithRetry performs an HTTP GET with rate limiting and retry on 429 responses.
func (c *Client) doGetWithRetry(ctx context.Context, reqURL string) ([]byte, error) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 || c.minDelay > 0 {
			delay := c.minDelay
			if attempt > 0 {
				// Exponential backoff: 2s, 4s, 8s
				delay = time.Second << attempt
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		body, err := c.do(ctx, http.MethodGet, reqURL, nil)
		if err == nil {
			return body, nil
		}

		if rle, ok := err.(*rateLimitError); ok && attempt < maxRetries {
			if secs, _ := strconv.Atoi(rle.retryAfter); secs > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(secs) * time.Second):
				}
			}
			continue
		}

		return nil, err
	}

	return nil, fmt.Errorf("max retries exceeded for %s", reqURL)
}

// do makes one request with c's credential and returns the body of a 2xx
// response.
func (c *Client) do(ctx context.Context, method, reqURL string, in io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, in)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.SetBasicAuth(c.email, c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return nil, &rateLimitError{
			statusCode: resp.StatusCode,
			retryAfter: retryAfter,
			body:       string(body[:min(len(body), 200)]),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("JIRA API returned %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	// Atlassian Cloud answers a rejected token with a 200 served anonymously,
	// which would look like an empty project and wipe the stored issues.
	if resp.Header.Get("X-Seraph-LoginReason") == "AUTHENTICATED_FAILED" {
		return nil, fmt.Errorf("JIRA API returned %d: token rejected (X-Seraph-LoginReason: AUTHENTICATED_FAILED)", http.StatusUnauthorized)
	}

	return body, nil
}

// rateLimitError represents a 429 Too Many Requests response.
type rateLimitError struct {
	statusCode int
	retryAfter string
	body       string
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("JIRA API returned %d: %s", e.statusCode, e.body)
}

// FixVersionToKonfluxApp maps a "{product}-v{version}" JIRA fixVersion to its
// Konflux application name, e.g. "omr-v2.0.10" → "omr-2-0".
func FixVersionToKonfluxApp(fixVersion string) string {
	if idx := strings.Index(fixVersion, "-v"); idx > 0 {
		product := fixVersion[:idx]
		version := fixVersion[idx+2:] // skip "-v"
		parts := strings.Split(version, ".")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s-%s-%s", product, parts[0], parts[1])
		}
	}
	return ""
}
