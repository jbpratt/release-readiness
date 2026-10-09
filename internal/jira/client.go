// Package jira provides a client for querying JIRA Cloud REST APIs.
package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds JIRA connection settings.
type Config struct {
	BaseURL        string // REST API base, e.g. https://redhat.atlassian.net
	SiteURL        string // browse-link base; defaults to BaseURL
	Email          string // JIRA Cloud account email for Basic Auth
	Token          string // JIRA Cloud API token
	Project        string // e.g. PROJQUAY
	QAContactField string // custom field name for QA Contact (e.g. customfield_12315948)
}

// Client is a JIRA REST API client.
type Client struct {
	baseURL        string
	siteURL        string
	email          string
	token          string
	project        string
	qaContactField string
	httpClient     *http.Client
	minDelay       time.Duration // minimum delay between requests
}

// New creates a new JIRA client.
func New(cfg Config) *Client {
	siteURL := cfg.SiteURL
	if siteURL == "" {
		siteURL = cfg.BaseURL
	}
	return &Client{
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		siteURL:        strings.TrimRight(siteURL, "/"),
		email:          cfg.Email,
		token:          cfg.Token,
		project:        cfg.Project,
		qaContactField: cfg.QAContactField,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		minDelay: 1 * time.Second,
	}
}

// Issue represents a JIRA issue from the REST API.
type Issue struct {
	Key       string      `json:"key"`
	Fields    IssueFields `json:"fields"`
	QAContact string      `json:"-"`
}

// IssueFields holds the fields we care about from a JIRA issue.
type IssueFields struct {
	Summary   string        `json:"summary"`
	Status    StatusField   `json:"status"`
	Priority  PriorityField `json:"priority"`
	Labels    []string      `json:"labels"`
	Assignee  *UserField    `json:"assignee"`
	IssueType TypeField     `json:"issuetype"`
	DueDate   string        `json:"duedate"`

	Raw map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes known fields and captures raw JSON for custom field extraction.
func (f *IssueFields) UnmarshalJSON(data []byte) error {
	type Alias IssueFields
	a := (*Alias)(f)
	if err := json.Unmarshal(data, a); err != nil {
		return err
	}
	return json.Unmarshal(data, &f.Raw)
}

type StatusField struct {
	Name string `json:"name"`
}

type PriorityField struct {
	Name string `json:"name"`
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
	fields := "summary,status,duedate,assignee"

	var allIssues []Issue
	nextPageToken := ""

	for {
		params := url.Values{
			"jql":        {jql},
			"fields":     {fields},
			"maxResults": {"100"},
		}
		if nextPageToken != "" {
			params.Set("nextPageToken", nextPageToken)
		}

		reqURL := fmt.Sprintf("%s/rest/api/3/search/jql?%s", c.baseURL, params.Encode())
		body, err := c.doGetWithRetry(ctx, reqURL)
		if err != nil {
			return nil, fmt.Errorf("discover releases: %w", err)
		}

		var resp searchResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode search response: %w", err)
		}

		allIssues = append(allIssues, resp.Issues...)

		if resp.NextPageToken == "" {
			break
		}
		nextPageToken = resp.NextPageToken
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

		if issue.Fields.DueDate != "" {
			t, err := time.Parse("2006-01-02", issue.Fields.DueDate)
			if err == nil {
				rel.DueDate = &t
			}
		}

		releases = append(releases, rel)
	}

	return releases, nil
}

// buildSearchJQL constructs the JQL for searching issues by Target Version.
func (c *Client) buildSearchJQL(version string) string {
	return fmt.Sprintf(`project=%s AND "Target Version"="%s"`,
		c.project, version)
}

// SearchIssues queries JIRA for issues matching a Target Version.
// It handles pagination automatically and respects rate limits.
func (c *Client) SearchIssues(ctx context.Context, fixVersion string) ([]Issue, error) {
	jql := c.buildSearchJQL(fixVersion)
	fields := "summary,status,priority,labels,assignee,issuetype"
	if c.qaContactField != "" {
		fields += "," + c.qaContactField
	}

	var allIssues []Issue
	nextPageToken := ""

	for {
		params := url.Values{
			"jql":        {jql},
			"fields":     {fields},
			"maxResults": {"100"},
		}
		if nextPageToken != "" {
			params.Set("nextPageToken", nextPageToken)
		}

		reqURL := fmt.Sprintf("%s/rest/api/3/search/jql?%s", c.baseURL, params.Encode())
		body, err := c.doGetWithRetry(ctx, reqURL)
		if err != nil {
			return nil, fmt.Errorf("search issues: %w", err)
		}

		var resp searchResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode search response: %w", err)
		}

		if c.qaContactField != "" {
			for i := range resp.Issues {
				if v, ok := resp.Issues[i].Fields.Raw[c.qaContactField]; ok {
					var u *UserField
					if json.Unmarshal(v, &u) == nil && u != nil {
						resp.Issues[i].QAContact = u.DisplayName
					}
				}
			}
		}

		allIssues = append(allIssues, resp.Issues...)

		if resp.NextPageToken == "" {
			break
		}
		nextPageToken = resp.NextPageToken
	}

	return allIssues, nil
}

// GetVersion fetches version metadata from JIRA for the given project and version name.
func (c *Client) GetVersion(ctx context.Context, versionName string) (*VersionField, error) {
	reqURL := fmt.Sprintf("%s/rest/api/3/project/%s/versions", c.baseURL, url.PathEscape(c.project))
	body, err := c.doGetWithRetry(ctx, reqURL)
	if err != nil {
		return nil, fmt.Errorf("get versions: %w", err)
	}

	var versions []VersionField
	if err := json.Unmarshal(body, &versions); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}

	for _, v := range versions {
		if v.Name == versionName {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("version %q not found in project %s", versionName, c.project)
}

// doGetWithRetry performs an HTTP GET with rate limiting and retry on 429 responses.
func (c *Client) doGetWithRetry(ctx context.Context, reqURL string) ([]byte, error) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 || c.minDelay > 0 {
			delay := c.minDelay
			if attempt > 0 {
				// Exponential backoff: 2s, 4s, 8s
				delay = time.Duration(math.Pow(2, float64(attempt))) * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		body, err := c.doGet(ctx, reqURL)
		if err == nil {
			return body, nil
		}

		// Check if it's a rate limit error
		if isRateLimitError(err) && attempt < maxRetries {
			retryAfter := parseRetryAfter(err)
			if retryAfter > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryAfter):
				}
			}
			continue
		}

		return nil, err
	}

	return nil, fmt.Errorf("max retries exceeded for %s", reqURL)
}

func (c *Client) doGet(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
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

	if resp.StatusCode != http.StatusOK {
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

func isRateLimitError(err error) bool {
	_, ok := err.(*rateLimitError)
	return ok
}

func parseRetryAfter(err error) time.Duration {
	rle, ok := err.(*rateLimitError)
	if !ok || rle.retryAfter == "" {
		return 0
	}
	seconds, err := strconv.Atoi(rle.retryAfter)
	if err != nil {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

var streamVersion = regexp.MustCompile(`^([a-z]+-v\d+\.\d+)\.\d+$`)

// StreamVersion returns the generic Target Version of fixVersion's stream,
// "quay-v3.16.2" → "quay-v3.16.z", or "" when fixVersion is not product-vX.Y.Z.
func StreamVersion(fixVersion string) string {
	m := streamVersion.FindStringSubmatch(fixVersion)
	if m == nil {
		return ""
	}
	return m[1] + ".z"
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
