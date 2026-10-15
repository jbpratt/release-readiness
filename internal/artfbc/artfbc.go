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

// Slot is an operator's FBC for an OCP version that a release needs. Staged
// is the digest of the FBC RR staged for it, "" for none, with StagedNVR, its
// ART NVR, "" when unknown.
type Slot struct {
	Operator, OCP, Staged, StagedNVR string
}

// Staged returns the multi-arch tag of operator's FBC of version for slot's
// OCP version whose digest is the slot's staged one. A tag is chosen by digest
// only: when several carry it, the one the slot's staged NVR names, else the
// newest. It returns false when there is none, or no staged digest.
func Staged(tags []Tag, operator, version string, slot Slot) (Tag, bool) {
	if slot.Staged == "" {
		return Tag{}, false
	}
	re := TagPattern(operator, version)
	var best Tag
	found := false
	for _, t := range tags {
		if m := re.FindStringSubmatch(t.Name); m == nil || m[1] != slot.OCP || t.Digest != slot.Staged {
			continue
		}
		switch {
		case !found:
		case best.Name == slot.StagedNVR:
			continue
		case t.Name != slot.StagedNVR && (t.Start < best.Start || t.Start == best.Start && t.Name < best.Name):
			continue
		}
		best, found = t, true
	}
	return best, found
}

// Tags returns the active tags of operator's FBC builds of version, newest
// first, every architecture's and the multi-arch ones, enough to answer
// slots, which are operator's own: the newest multi-arch tag of each slot's
// OCP version, and for each slot with a staged digest the tag that points at
// it. A staged NVR that names such a tag is read first, on its own. It then
// reads pages, as Quay lists the newest first, until every slot is answered
// or there are no more or it has read its page limit; a slot it found no tag
// for is for the caller to report.
func (c *Client) Tags(ctx context.Context, operator, version string, slots []Slot) ([]Tag, error) {
	keys := make([]string, len(slots))
	for i, s := range slots {
		keys[i] = s.OCP + "=" + s.Staged
	}
	key := "tags/" + operator + "/" + version + "/" + strings.Join(slices.Sorted(slices.Values(keys)), ",")
	if e, ok := c.cacheGet(key); ok {
		return slices.Clone(e.tags), nil
	}
	var tags []Tag
	re := TagPattern(operator, version)
	for _, s := range slots {
		m := re.FindStringSubmatch(s.StagedNVR)
		if s.Staged == "" || m == nil || m[1] != s.OCP || slices.ContainsFunc(tags, func(t Tag) bool { return t.Name == s.StagedNVR }) {
			continue
		}
		t, ok, err := c.tag(ctx, s.StagedNVR)
		if err != nil {
			return nil, fmt.Errorf("%s FBC tag %s: %w", operator, s.StagedNVR, err)
		}
		if ok {
			tags = append(tags, t)
		}
	}
	for page := 1; page <= c.maxPages; page++ {
		q := url.Values{
			"filter_tag_name": {"like:" + operator + "-fbc-" + version},
			"onlyActiveTags":  {"true"},
			"limit":           {fmt.Sprint(c.pageSize)},
			"page":            {fmt.Sprint(page)},
		}
		rows, more, err := c.tagPage(ctx, operator, version, q)
		if err != nil {
			return nil, err
		}
		for _, t := range rows {
			if !slices.ContainsFunc(tags, func(o Tag) bool { return o.Name == t.Name }) {
				tags = append(tags, t)
			}
		}
		if !more || answered(tags, operator, version, slots) {
			break
		}
	}
	c.cachePut(key, cached{tags: tags})
	return slices.Clone(tags), nil
}

// answered tells whether tags hold the newest tag of every slot's OCP version
// and the staged tag of every slot with a staged digest.
func answered(tags []Tag, operator, version string, slots []Slot) bool {
	newest := Newest(tags, operator, version)
	return !slices.ContainsFunc(slots, func(s Slot) bool {
		if _, ok := newest[s.OCP]; !ok {
			return true
		}
		_, ok := Staged(tags, operator, version, s)
		return s.Staged != "" && !ok
	})
}

// tag reads the active tag named name, false when there is none.
func (c *Client) tag(ctx context.Context, name string) (Tag, bool, error) {
	rows, _, err := c.tagPage(ctx, "", "", url.Values{"specificTag": {name}, "onlyActiveTags": {"true"}})
	if err != nil {
		return Tag{}, false, err
	}
	for _, t := range rows {
		if t.Name == name {
			return t, true, nil
		}
	}
	return Tag{}, false, nil
}

// tagPage reads one page of the repository's tags for the query, and whether
// there are more.
func (c *Client) tagPage(ctx context.Context, operator, version string, q url.Values) ([]Tag, bool, error) {
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
		return nil, false, err
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("%s FBC tags of %s: %w", operator, version, err)
	}
	rows := make([]Tag, len(resp.Tags))
	for i, t := range resp.Tags {
		rows[i] = Tag{Name: t.Name, Digest: t.ManifestDigest, Start: t.StartTS}
	}
	return rows, resp.HasAdditional, nil
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

// Resolved is the tag chosen for a slot. Basis is "staged" when the tag's
// digest is the one RR staged for the slot, and "newest" when RR staged none
// and the newest tag stands in. NewerTag names the newest tag when it is a
// build ART made after the staged one, "" for none.
type Resolved struct {
	Operator     string `json:"operator"`
	OCP          string `json:"ocp"`
	Tag          string `json:"tag"`
	Pullspec     string `json:"pullspec"`
	Digest       string `json:"digest"`
	StagedDigest string `json:"staged_digest"`
	StagedNVR    string `json:"staged_nvr"`
	Basis        string `json:"basis"`
	NewerTag     string `json:"newer_tag"`
}

// Result is what Resolve decided: Tags holds the slots that have a tag, in
// slot order; Pullspecs their pullspecs, empty when any slot has none, as a
// partial list must not be released; Errors the slots without one; Warnings
// the slots whose newest tag stands in for a staged one RR has none of; and
// Info the slots ART has built again since RR staged them.
type Result struct {
	Tags      []Resolved
	Pullspecs []string
	Errors    []string
	Warnings  []string
	Info      []string
}

// Resolve picks for each slot, in order, the tag of its operator's FBC of
// version, from tags by operator. A slot with a staged digest takes the tag of
// that digest, and is an error without one: the newest tag is never taken in
// its place, only noted when it is a newer build. A slot with no staged digest
// takes the newest tag, with a warning.
func Resolve(slots []Slot, version string, tags map[string][]Tag) Result {
	r := Result{Tags: []Resolved{}, Pullspecs: []string{}, Errors: []string{}, Warnings: []string{}, Info: []string{}}
	if len(slots) == 0 {
		r.Errors = append(r.Errors, "no operator FBC to look up")
		return r
	}
	newest := map[string]map[string]Tag{}
	for _, s := range slots {
		if _, ok := newest[s.Operator]; !ok {
			newest[s.Operator] = Newest(tags[s.Operator], s.Operator, version)
		}
	}
	for _, s := range slots {
		res := Resolved{Operator: s.Operator, OCP: s.OCP, StagedDigest: s.Staged, StagedNVR: s.StagedNVR}
		latest, haveLatest := newest[s.Operator][s.OCP]
		var t Tag
		switch {
		case s.Staged != "":
			var ok bool
			if t, ok = Staged(tags[s.Operator], s.Operator, version, s); !ok {
				r.Errors = append(r.Errors, fmt.Sprintf("%s OCP %s: no tag in %s points at the staged digest %s (searched %d tags)",
					s.Operator, s.OCP, Repository, s.Staged, len(tags[s.Operator])))
				continue
			}
			res.Basis = "staged"
			if haveLatest && latest.Name != t.Name {
				res.NewerTag = latest.Name
				r.Info = append(r.Info, fmt.Sprintf("%s OCP %s: ART has a newer build: %s", s.Operator, s.OCP, latest.Name))
			}
		case !haveLatest:
			r.Errors = append(r.Errors, fmt.Sprintf("%s OCP %s: no tag in %s", s.Operator, s.OCP, Repository))
			continue
		default:
			t = latest
			res.Basis = "newest"
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s OCP %s: newest ART build, not staged in RR (%s)", s.Operator, s.OCP, t.Name))
		}
		res.Tag, res.Pullspec, res.Digest = t.Name, Pullspec(t), t.Digest
		r.Tags = append(r.Tags, res)
	}
	if len(r.Errors) == 0 {
		for _, t := range r.Tags {
			r.Pullspecs = append(r.Pullspecs, t.Pullspec)
		}
	}
	return r
}
