// Package artfbc lists the FBC images ART publishes to its public
// quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc repository. Each build
// gets a tag named <operator>-fbc-<version>-<timestamp>.ocp<X.Y>, unique to
// the build, that release-from-fbc takes as an fbc-pullspec.
package artfbc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

const (
	repoPath = "redhat-user-workloads/ocp-art-tenant/art-fbc"
	// Repository is the public repository ART publishes its FBC images to.
	Repository = "quay.io/" + repoPath

	// DefaultQuayURL and DefaultBuildDataURL are the public services read.
	DefaultQuayURL      = "https://quay.io"
	DefaultBuildDataURL = "https://raw.githubusercontent.com/openshift-eng/ocp-build-data"

	defaultPageSize = 100
	// defaultMaxPages bounds a listing. ART tags every rebuild of a version
	// and each architecture of it, so a version can hold over a thousand
	// tags, but its newest are first.
	defaultMaxPages = 10
	defaultTTL      = 2 * time.Minute
)

// Tag is a tag of the repository, with the digest of its manifest and the
// time it started, in Unix seconds.
type Tag struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Start  int64  `json:"start"`
}

// TagPattern matches the tags of operator's FBC builds of version, as
// quay-operator-fbc-3.18.2-20261015132328.ocp4.16, and returns the OCP version
// as its one group. The tags of each architecture, such as ...ocp4.16-linux-arm64,
// and a tag without a timestamp, which moves with the build, do not match.
func TagPattern(operator, version string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(operator+"-fbc-"+version) + `-\d{14}\.ocp(\d+\.\d+)$`)
}

// Newest returns, by OCP version, the newest tag, by start time, of operator's
// FBC builds of version in tags.
func Newest(tags []Tag, operator, version string) map[string]Tag {
	re := TagPattern(operator, version)
	newest := map[string]Tag{}
	for _, t := range tags {
		m := re.FindStringSubmatch(t.Name)
		if m == nil {
			continue
		}
		if cur, ok := newest[m[1]]; !ok || t.Start > cur.Start || t.Start == cur.Start && t.Name > cur.Name {
			newest[m[1]] = t
		}
	}
	return newest
}

// Pullspec is the pullspec of the image a tag names.
func Pullspec(t Tag) string { return Repository + ":" + t.Name }

// Client reads the repository's tags from the Quay API without credentials,
// and the OCP versions a stream targets from ocp-build-data. It keeps what it
// read for a short while, as the page is read more often than ART builds.
type Client struct {
	quayURL, buildDataURL string
	http                  *http.Client
	pageSize, maxPages    int
	ttl                   time.Duration
	now                   func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	tags []Tag
	ocps []string
}

// NewClient returns a Client reading tags from the Quay API at quayURL and
// group.yml files from the raw ocp-build-data at buildDataURL.
func NewClient(quayURL, buildDataURL string, hc *http.Client) *Client {
	return &Client{
		quayURL:      strings.TrimSuffix(quayURL, "/"),
		buildDataURL: strings.TrimSuffix(buildDataURL, "/"),
		http:         hc,
		pageSize:     defaultPageSize,
		maxPages:     defaultMaxPages,
		ttl:          defaultTTL,
		now:          time.Now,
		cache:        map[string]cached{},
	}
}

func (c *Client) cacheGet(key string) (cached, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	return e, ok && c.now().Sub(e.at) < c.ttl
}

func (c *Client) cachePut(key string, e cached) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.at = c.now()
	c.cache[key] = e
}

// Tags returns the active tags of operator's FBC builds of version, newest
// first, every architecture's and the multi-arch ones. It reads pages until
// every one of ocps has a multi-arch tag, as Quay lists the newest first, or
// there are no more or it has read its page limit; an OCP version it found no
// tag of is for the caller to report.
func (c *Client) Tags(ctx context.Context, operator, version string, ocps []string) ([]Tag, error) {
	key := "tags/" + operator + "/" + version + "/" + strings.Join(slices.Sorted(slices.Values(ocps)), ",")
	if e, ok := c.cacheGet(key); ok {
		return slices.Clone(e.tags), nil
	}
	var tags []Tag
	for page := 1; page <= c.maxPages; page++ {
		q := url.Values{
			"filter_tag_name": {"like:" + operator + "-fbc-" + version},
			"onlyActiveTags":  {"true"},
			"limit":           {fmt.Sprint(c.pageSize)},
			"page":            {fmt.Sprint(page)},
		}
		var resp struct {
			Tags []struct {
				Name           string `json:"name"`
				ManifestDigest string `json:"manifest_digest"`
				StartTS        int64  `json:"start_ts"`
			} `json:"tags"`
			HasAdditional bool `json:"has_additional"`
		}
		body, err := c.get(ctx, c.quayURL+"/api/v1/repository/"+repoPath+"/tag/?"+q.Encode())
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("%s FBC tags of %s: %w", operator, version, err)
		}
		for _, t := range resp.Tags {
			tags = append(tags, Tag{Name: t.Name, Digest: t.ManifestDigest, Start: t.StartTS})
		}
		if !resp.HasAdditional {
			break
		}
		found := Newest(tags, operator, version)
		if !slices.ContainsFunc(ocps, func(ocp string) bool { _, ok := found[ocp]; return !ok }) {
			break
		}
	}
	c.cachePut(key, cached{tags: tags})
	return slices.Clone(tags), nil
}

// TargetVersions returns the OCP versions ART targets for stream, as 3.18, from
// OCP_TARGET_VERSIONS in its ocp-build-data group.yml.
func (c *Client) TargetVersions(ctx context.Context, stream string) ([]string, error) {
	key := "group/" + stream
	if e, ok := c.cacheGet(key); ok {
		return slices.Clone(e.ocps), nil
	}
	body, err := c.get(ctx, c.buildDataURL+"/quay-"+stream+"/group.yml")
	if err != nil {
		return nil, err
	}
	var group struct {
		Targets []string `json:"OCP_TARGET_VERSIONS"`
	}
	if err := yaml.Unmarshal(body, &group); err != nil {
		return nil, fmt.Errorf("group.yml of quay-%s: %w", stream, err)
	}
	if len(group.Targets) == 0 {
		return nil, fmt.Errorf("group.yml of quay-%s has no OCP_TARGET_VERSIONS", stream)
	}
	c.cachePut(key, cached{ocps: group.Targets})
	return slices.Clone(group.Targets), nil
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Slot is an operator's FBC for an OCP version that a release needs. Staged
// is the digest of the candidate RR knows for it, "" for none, with StagedNVR,
// its ART NVR, "" when unknown.
type Slot struct {
	Operator, OCP, Staged, StagedNVR string
}

// Resolved is the tag chosen for a slot. Match is "match" when the tag's
// digest is the slot's staged one, "mismatch" when it is not, and "unknown"
// when RR knows no staged digest.
type Resolved struct {
	Operator     string `json:"operator"`
	OCP          string `json:"ocp"`
	Tag          string `json:"tag"`
	Pullspec     string `json:"pullspec"`
	Digest       string `json:"digest"`
	StagedDigest string `json:"staged_digest"`
	StagedNVR    string `json:"staged_nvr"`
	Match        string `json:"match"`
}

// Result is what Resolve decided: Tags holds the slots that have a tag, in
// slot order; Pullspecs their pullspecs, empty when any slot has none, as a
// partial list must not be released; Errors the slots without one; and
// Warnings the slots whose tag is not the staged candidate.
type Result struct {
	Tags      []Resolved
	Pullspecs []string
	Errors    []string
	Warnings  []string
}

// Resolve picks for each slot, in order, the tag of its operator and OCP
// version in newest, and checks it against the slot's staged digest.
func Resolve(slots []Slot, newest map[string]map[string]Tag) Result {
	r := Result{Tags: []Resolved{}, Pullspecs: []string{}, Errors: []string{}, Warnings: []string{}}
	if len(slots) == 0 {
		r.Errors = append(r.Errors, "no operator FBC to look up")
		return r
	}
	for _, s := range slots {
		t, ok := newest[s.Operator][s.OCP]
		if !ok {
			r.Errors = append(r.Errors, fmt.Sprintf("%s OCP %s: no tag in %s", s.Operator, s.OCP, Repository))
			continue
		}
		res := Resolved{
			Operator: s.Operator, OCP: s.OCP, Tag: t.Name, Pullspec: Pullspec(t), Digest: t.Digest,
			StagedDigest: s.Staged, StagedNVR: s.StagedNVR, Match: "unknown",
		}
		switch {
		case s.Staged == "":
		case s.Staged == t.Digest:
			res.Match = "match"
		default:
			res.Match = "mismatch"
			staged := "the staged candidate"
			if s.StagedNVR != "" {
				staged += " " + s.StagedNVR
			}
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s OCP %s: ART has a newer build than the staged candidate: tag %s is %s, %s is %s",
				s.Operator, s.OCP, t.Name, t.Digest, staged, s.Staged))
		}
		r.Tags = append(r.Tags, res)
	}
	if len(r.Errors) == 0 {
		for _, t := range r.Tags {
			r.Pullspecs = append(r.Pullspecs, t.Pullspec)
		}
	}
	return r
}
