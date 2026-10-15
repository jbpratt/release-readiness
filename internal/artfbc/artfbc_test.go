package artfbc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tagAPI = "/api/v1/repository/redhat-user-workloads/ocp-art-tenant/art-fbc/tag/"

// fakeQuay serves tags like quay.io's tag API, filtered by filter_tag_name
// like:<substring> and paged by page and limit, and the stream's group.yml.
type fakeQuay struct {
	*httptest.Server
	tags     []quayTag
	requests atomic.Int32
}

type quayTag struct {
	Name           string `json:"name"`
	ManifestDigest string `json:"manifest_digest"`
	StartTS        int64  `json:"start_ts"`
}

func newFakeQuay(t *testing.T, tags []quayTag) *fakeQuay {
	t.Helper()
	f := &fakeQuay{tags: tags}
	mux := http.NewServeMux()
	mux.HandleFunc(tagAPI, func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		q := r.URL.Query()
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "the repository is public", http.StatusBadRequest)
			return
		}
		sub, ok := strings.CutPrefix(q.Get("filter_tag_name"), "like:")
		if !ok || q.Get("onlyActiveTags") != "true" {
			http.Error(w, "bad query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		var match []quayTag
		for _, tg := range f.tags {
			if strings.Contains(tg.Name, sub) {
				match = append(match, tg)
			}
		}
		// Newest first, as Quay lists them.
		slices.SortFunc(match, func(a, b quayTag) int { return int(b.StartTS - a.StartTS) })
		limit, _ := strconv.Atoi(q.Get("limit"))
		page, _ := strconv.Atoi(q.Get("page"))
		page = max(page, 1)
		from := min((page-1)*limit, len(match))
		to := min(from+limit, len(match))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tags":%s,"page":%d,"has_additional":%t}`, mustJSON(t, match[from:to]), page, to < len(match))
	})
	mux.HandleFunc("/quay-3.18/group.yml", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("testdata/group.yml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeQuay) client() *Client {
	c := NewClient(f.URL, f.URL, f.Client())
	c.pageSize = 3
	return c
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("[")
	tags, _ := v.([]quayTag)
	for i, tg := range tags {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name":%q,"manifest_digest":%q,"start_ts":%d}`, tg.Name, tg.ManifestDigest, tg.StartTS)
	}
	b.WriteString("]")
	return b.String()
}

func digest(s string) string { return "sha256:" + strings.Repeat(s, 64)[:64] }

// The pattern keeps the multi-arch, timestamped tag of the operator's FBC of
// the version, and nothing else: not an architecture's tag, a base tag
// without a timestamp, another operator, or another version that merely
// starts with it.
func TestTagPattern(t *testing.T) {
	re := TagPattern("quay-operator", "3.18.2")
	for tag, want := range map[string]string{
		"quay-operator-fbc-3.18.2-20261015132328.ocp4.16":               "4.16",
		"quay-operator-fbc-3.18.2-20261015132257.ocp5.0":                "5.0",
		"quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-arm64":   "",
		"quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-s390x":   "",
		"quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-x86-64":  "",
		"quay-operator-fbc-3.18.2.ocp4.16":                              "",
		"quay-operator-fbc-3.18.2":                                      "",
		"quay-operator-fbc-3.18.2-latest":                               "",
		"quay-operator-fbc-3.18.20-20261015132328.ocp4.16":              "",
		"quay-operator-fbc-3.18.2-2026101513232.ocp4.16":                "",
		"quay-bridge-operator-fbc-3.18.2-20261015132328.ocp4.16":        "",
		"container-security-operator-fbc-3.18.2-20261015132328.ocp4.16": "",
	} {
		got := ""
		if m := re.FindStringSubmatch(tag); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%s: OCP %q, want %q", tag, got, want)
		}
	}
}

// Of an operator's tags, Newest keeps for each OCP version the newest build's
// tag by start time, whatever the order, and not the arch tags, which can be
// newer than the multi-arch tag by a minute.
func TestNewest(t *testing.T) {
	tags := []Tag{
		{Name: "quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-arm64", Digest: digest("a"), Start: 9000},
		{Name: "quay-operator-fbc-3.18.2-20261014000000.ocp4.16", Digest: digest("b"), Start: 100},
		{Name: "quay-operator-fbc-3.18.2-20261015132328.ocp4.16", Digest: digest("c"), Start: 300},
		{Name: "quay-operator-fbc-3.18.2-20261015132257.ocp4.14", Digest: digest("d"), Start: 200},
		{Name: "quay-operator-fbc-3.18.2", Digest: digest("e"), Start: 9999},
		{Name: "quay-operator-fbc-3.18.20-20261016000000.ocp4.14", Digest: digest("f"), Start: 9998},
		{Name: "quay-operator-fbc-3.18.2-20261013000000.ocp4.14", Digest: digest("g"), Start: 50},
	}
	got := Newest(tags, "quay-operator", "3.18.2")
	want := map[string]Tag{
		"4.14": {Name: "quay-operator-fbc-3.18.2-20261015132257.ocp4.14", Digest: digest("d"), Start: 200},
		"4.16": {Name: "quay-operator-fbc-3.18.2-20261015132328.ocp4.16", Digest: digest("c"), Start: 300},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Newest = %+v, want %+v", got, want)
	}
}

func TestPullspec(t *testing.T) {
	got := Pullspec(Tag{Name: "quay-operator-fbc-3.18.2-20261015132328.ocp4.16"})
	want := "quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc:quay-operator-fbc-3.18.2-20261015132328.ocp4.16"
	if got != want {
		t.Errorf("Pullspec = %q, want %q", got, want)
	}
}

func fixtureTags() []quayTag {
	return []quayTag{
		{"quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-arm64", digest("1"), 1792071662},
		{"quay-operator-fbc-3.18.2-20261015132328.ocp4.16", digest("2"), 1792071720},
		{"quay-operator-fbc-3.18.2-20261015132257.ocp4.14", digest("3"), 1792071599},
		{"quay-operator-fbc-3.18.2-20261015132305.ocp4.15", digest("4"), 1792071561},
		{"quay-operator-fbc-3.18.2-20261010000000.ocp4.15", digest("5"), 1791600000},
		{"quay-operator-fbc-3.18.2-20261015132257.ocp4.14-linux-s390x", digest("6"), 1792071542},
		{"quay-operator-fbc-3.18.20-20261016000000.ocp4.14", digest("7"), 1792100000},
		{"quay-bridge-operator-fbc-3.18.2-20261012232756.ocp4.14", digest("8"), 1791849305},
	}
}

var allOCPs = []string{"4.14", "4.15", "4.16"}

// Tags pages through the listing, 3 a page here, asks for active tags only and
// sends no credentials. It stops once every wanted OCP version has a tag, here
// after 2 pages of the 7 that match, with the newest tags among those read.
func TestTags(t *testing.T) {
	f := newFakeQuay(t, fixtureTags())
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", allOCPs)
	if err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("%d requests, want 2 pages: every OCP version has a tag by then", n)
	}
	got := Newest(tags, "quay-operator", "3.18.2")
	var names []string
	for _, ocp := range allOCPs {
		names = append(names, got[ocp].Name)
	}
	want := []string{
		"quay-operator-fbc-3.18.2-20261015132257.ocp4.14",
		"quay-operator-fbc-3.18.2-20261015132305.ocp4.15",
		"quay-operator-fbc-3.18.2-20261015132328.ocp4.16",
	}
	if !slices.Equal(names, want) {
		t.Errorf("newest = %v, want %v", names, want)
	}
}

// An OCP version with no tag makes Tags read every page, and is not an error
// here: Resolve reports it. A page limit stops it earlier, also without one.
func TestTagsMissingOCP(t *testing.T) {
	f := newFakeQuay(t, fixtureTags())
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", append([]string{"4.17"}, allOCPs...))
	if err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 3 {
		t.Errorf("%d requests, want all 3 pages", n)
	}
	if _, ok := Newest(tags, "quay-operator", "3.18.2")["4.17"]; ok {
		t.Error("found a tag for OCP 4.17")
	}

	f = newFakeQuay(t, fixtureTags())
	c := f.client()
	c.maxPages = 1
	if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", append([]string{"4.17"}, allOCPs...)); err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 1 {
		t.Errorf("%d requests, want the page limit of 1", n)
	}
}

// A second lookup within the cache's lifetime makes no request; one after it
// does.
func TestTagsCache(t *testing.T) {
	f := newFakeQuay(t, fixtureTags())
	c := f.client()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	for range 2 {
		if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", allOCPs); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("%d requests after two lookups, want 2", n)
	}
	now = now.Add(c.ttl + time.Second)
	if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", allOCPs); err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 4 {
		t.Errorf("%d requests after the cache expired, want 4", n)
	}
}

func TestTagsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, srv.URL, srv.Client()).Tags(t.Context(), "quay-operator", "3.18.2", allOCPs); err == nil {
		t.Error("Tags succeeded against a failing Quay, want an error")
	}
}

func TestTargetVersions(t *testing.T) {
	f := newFakeQuay(t, nil)
	got, err := f.client().TargetVersions(t.Context(), "3.18")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"4.12", "4.14", "4.15", "4.16", "4.17", "4.18", "4.19", "4.20", "4.21", "4.22", "5.0"}
	if !slices.Equal(got, want) {
		t.Errorf("TargetVersions = %v, want %v", got, want)
	}
	if _, err := f.client().TargetVersions(t.Context(), "9.9"); err == nil {
		t.Error("TargetVersions of a stream without a group.yml succeeded, want an error")
	}
}

func tag(operator, ocp, ts string, d string) Tag {
	return Tag{Name: operator + "-fbc-3.18.2-" + ts + ".ocp" + ocp, Digest: digest(d), Start: 1}
}

// Resolve gives each slot its newest tag's pullspec, in slot order, and a
// warning for a slot whose staged digest is not the tag's, naming both.
func TestResolve(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14", Staged: digest("3"), StagedNVR: "quay-operator-fbc-3.18.2-20261015132257.ocp4.14"},
		{Operator: "quay-operator", OCP: "4.16", Staged: digest("9"), StagedNVR: "quay-operator-fbc-3.18.2-20261014000000.ocp4.16"},
		{Operator: "quay-bridge-operator", OCP: "4.14"},
	}
	newest := map[string]map[string]Tag{
		"quay-operator":        {"4.14": tag("quay-operator", "4.14", "20261015132257", "3"), "4.16": tag("quay-operator", "4.16", "20261015132328", "2")},
		"quay-bridge-operator": {"4.14": tag("quay-bridge-operator", "4.14", "20261012232756", "8")},
	}
	got := Resolve(slots, newest)
	wantSpecs := []string{
		"quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc:quay-operator-fbc-3.18.2-20261015132257.ocp4.14",
		"quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc:quay-operator-fbc-3.18.2-20261015132328.ocp4.16",
		"quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc:quay-bridge-operator-fbc-3.18.2-20261012232756.ocp4.14",
	}
	if !slices.Equal(got.Pullspecs, wantSpecs) {
		t.Errorf("Pullspecs = %v, want %v", got.Pullspecs, wantSpecs)
	}
	if len(got.Errors) != 0 {
		t.Errorf("Errors = %v, want none", got.Errors)
	}
	wantWarn := []string{
		"quay-operator OCP 4.16: ART has a newer build than the staged candidate: tag quay-operator-fbc-3.18.2-20261015132328.ocp4.16 is " +
			digest("2") + ", the staged candidate quay-operator-fbc-3.18.2-20261014000000.ocp4.16 is " + digest("9"),
	}
	if !slices.Equal(got.Warnings, wantWarn) {
		t.Errorf("Warnings = %q, want %q", got.Warnings, wantWarn)
	}
	var matches []string
	for _, r := range got.Tags {
		matches = append(matches, fmt.Sprintf("%s/%s=%s", r.Operator, r.OCP, r.Match))
	}
	wantMatch := []string{"quay-operator/4.14=match", "quay-operator/4.16=mismatch", "quay-bridge-operator/4.14=unknown"}
	if !slices.Equal(matches, wantMatch) {
		t.Errorf("matches = %v, want %v", matches, wantMatch)
	}
}

// A slot without a tag is an error naming the operator and OCP version, and
// the pullspecs are empty: a partial list must not be released.
func TestResolveMissing(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14"},
		{Operator: "quay-operator", OCP: "4.16"},
		{Operator: "container-security-operator", OCP: "4.14"},
	}
	newest := map[string]map[string]Tag{
		"quay-operator": {"4.14": tag("quay-operator", "4.14", "20261015132257", "3")},
	}
	got := Resolve(slots, newest)
	wantErr := []string{
		"quay-operator OCP 4.16: no tag in " + Repository,
		"container-security-operator OCP 4.14: no tag in " + Repository,
	}
	if !slices.Equal(got.Errors, wantErr) {
		t.Errorf("Errors = %q, want %q", got.Errors, wantErr)
	}
	if len(got.Pullspecs) != 0 {
		t.Errorf("Pullspecs = %v, want none with errors", got.Pullspecs)
	}
	if len(got.Tags) != 1 {
		t.Errorf("Tags = %+v, want the one that was found", got.Tags)
	}
}

func TestResolveNoSlots(t *testing.T) {
	got := Resolve(nil, nil)
	if len(got.Errors) != 1 || len(got.Pullspecs) != 0 {
		t.Errorf("Resolve(nil) = %+v, want one error and no pullspecs", got)
	}
}
