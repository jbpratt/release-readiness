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
	tags []quayTag
	// requests counts listing pages read, specific the lookups of one tag.
	requests, specific atomic.Int32
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
		q := r.URL.Query()
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "the repository is public", http.StatusBadRequest)
			return
		}
		if name := q.Get("specificTag"); name != "" {
			f.specific.Add(1)
			var match []quayTag
			for _, tg := range f.tags {
				if tg.Name == name {
					match = append(match, tg)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"tags":%s,"page":1,"has_additional":false}`, mustJSON(t, match))
			return
		}
		f.requests.Add(1)
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
		_, _ = fmt.Fprintf(w, `{"tags":%s,"page":%d,"has_additional":%t}`, mustJSON(t, match[from:to]), page, to < len(match))
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

// slotsOf is operator's unstaged slots for ocps.
func slotsOf(operator string, ocps ...string) []Slot {
	var slots []Slot
	for _, ocp := range ocps {
		slots = append(slots, Slot{Operator: operator, OCP: ocp})
	}
	return slots
}

// Tags pages through the listing, 3 a page here, asks for active tags only and
// sends no credentials. It stops once every wanted OCP version has a tag, here
// after 2 pages of the 7 that match, with the newest tags among those read.
func TestTags(t *testing.T) {
	f := newFakeQuay(t, fixtureTags())
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", allOCPs...))
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
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", append([]string{"4.17"}, allOCPs...)...))
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
	if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", append([]string{"4.17"}, allOCPs...)...)); err != nil {
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
		if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", allOCPs...)); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("%d requests after two lookups, want 2", n)
	}
	now = now.Add(c.ttl + time.Second)
	if _, err := c.Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", allOCPs...)); err != nil {
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
	if _, err := NewClient(srv.URL, srv.URL, srv.Client()).Tags(t.Context(), "quay-operator", "3.18.2", slotsOf("quay-operator", allOCPs...)); err == nil {
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

// stagedFixture holds two builds of quay-operator 3.18.2 for OCP 4.14 and
// 4.16: the old one RR staged, whose tags are the oldest, and the new one ART
// built since. With 3 tags a page, newest first, the staged tags are on page 2.
func stagedFixture() []quayTag {
	return []quayTag{
		{"quay-operator-fbc-3.18.2-20261015132328.ocp4.16", digest("2"), 1792071720},
		{"quay-operator-fbc-3.18.2-20261015132328.ocp4.16-linux-arm64", digest("1"), 1792071662},
		{"quay-operator-fbc-3.18.2-20261015132257.ocp4.14", digest("3"), 1792071599},
		{"quay-operator-fbc-3.18.2-20261010000000.ocp4.16", digest("9"), 1791600000},
		{"quay-operator-fbc-3.18.2-20261010000000.ocp4.16-linux-arm64", digest("a"), 1791599900},
		{"quay-operator-fbc-3.18.2-20261005000000.ocp4.14", digest("b"), 1791200000},
	}
}

const (
	oldQO16 = "quay-operator-fbc-3.18.2-20261010000000.ocp4.16"
	newQO16 = "quay-operator-fbc-3.18.2-20261015132328.ocp4.16"
)

func tagNames(tags []Tag) []string {
	var names []string
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}

// Tags keeps reading pages until it has found the tag of each staged digest
// as well as the newest tag of each OCP version, here on page 2, and stops
// there; the page limit stops it earlier, without an error.
func TestTagsStagedOnLaterPage(t *testing.T) {
	f := newFakeQuay(t, stagedFixture())
	slots := []Slot{{Operator: "quay-operator", OCP: "4.16", Staged: digest("9")}, {Operator: "quay-operator", OCP: "4.14"}}
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", slots)
	if err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("%d pages, want 2: the staged tag is on page 2, and the read stops there", n)
	}
	if got, ok := Staged(tags, "quay-operator", "3.18.2", slots[0]); !ok || got.Name != oldQO16 {
		t.Errorf("staged tag = %q, %v; want %s", got.Name, ok, oldQO16)
	}

	f = newFakeQuay(t, stagedFixture())
	c := f.client()
	c.maxPages = 1
	tags, err = c.Tags(t.Context(), "quay-operator", "3.18.2", slots)
	if err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 1 {
		t.Errorf("%d pages, want the page limit of 1", n)
	}
	if _, ok := Staged(tags, "quay-operator", "3.18.2", slots[0]); ok {
		t.Error("found the staged tag beyond the page limit")
	}
}

// A slot's staged NVR that names a tag whose digest is the staged one is read
// with one lookup of that tag, and the listing stops at the newest tags.
func TestTagsStagedNVRShortcut(t *testing.T) {
	f := newFakeQuay(t, stagedFixture())
	slot := Slot{Operator: "quay-operator", OCP: "4.16", Staged: digest("9"), StagedNVR: oldQO16}
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", []Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	if f.specific.Load() != 1 || f.requests.Load() != 1 {
		t.Errorf("%d tag lookups and %d pages, want 1 and 1: the NVR names the staged tag", f.specific.Load(), f.requests.Load())
	}
	if got, ok := Staged(tags, "quay-operator", "3.18.2", slot); !ok || got.Name != oldQO16 {
		t.Errorf("staged tag = %q, %v; want %s", got.Name, ok, oldQO16)
	}
	if n := slices.Sorted(slices.Values(tagNames(tags))); len(n) != len(slices.Compact(slices.Clone(n))) {
		t.Errorf("tags repeat: %v", n)
	}
}

// An NVR that names a tag with another digest is not trusted: the staged
// digest is looked up in the listing, and the NVR's tag is not the answer.
func TestTagsStagedNVRRejected(t *testing.T) {
	f := newFakeQuay(t, stagedFixture())
	slot := Slot{Operator: "quay-operator", OCP: "4.16", Staged: digest("9"), StagedNVR: newQO16}
	tags, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", []Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	if f.specific.Load() != 1 || f.requests.Load() != 2 {
		t.Errorf("%d tag lookups and %d pages, want 1 and 2: the lookup is rejected, so the listing finds the digest", f.specific.Load(), f.requests.Load())
	}
	if got, ok := Staged(tags, "quay-operator", "3.18.2", slot); !ok || got.Name != oldQO16 {
		t.Errorf("staged tag = %q, %v; want %s, not the NVR's tag", got.Name, ok, oldQO16)
	}
	// An NVR that is no tag of the operator's builds is not looked up.
	f = newFakeQuay(t, stagedFixture())
	slot.StagedNVR = "quay-operator-metadata-container-3.18.2.202610101200"
	if _, err := f.client().Tags(t.Context(), "quay-operator", "3.18.2", []Slot{slot}); err != nil {
		t.Fatal(err)
	}
	if f.specific.Load() != 0 {
		t.Errorf("%d tag lookups of an NVR that is no FBC tag, want 0", f.specific.Load())
	}
}

// Staged finds the multi-arch tag whose digest is the slot's, in its operator,
// version and OCP version only: never a per-architecture tag with that digest,
// another OCP version's, or a tag named by the NVR whose digest differs.
func TestStaged(t *testing.T) {
	tags := []Tag{
		{Name: newQO16, Digest: digest("2"), Start: 300},
		{Name: oldQO16 + "-linux-arm64", Digest: digest("9"), Start: 250},
		{Name: "quay-operator-fbc-3.18.2-20261010000000.ocp4.14", Digest: digest("9"), Start: 240},
		{Name: oldQO16, Digest: digest("9"), Start: 200},
		{Name: "quay-operator-fbc-3.18.2-20261009000000.ocp4.16", Digest: digest("9"), Start: 100},
	}
	slot := Slot{Operator: "quay-operator", OCP: "4.16", Staged: digest("9")}
	got, ok := Staged(tags, "quay-operator", "3.18.2", slot)
	if !ok || got.Name != oldQO16 {
		t.Errorf("Staged = %q, %v; want the newest multi-arch tag of the digest, %s", got.Name, ok, oldQO16)
	}
	// Two tags carry the digest: the one the NVR names wins.
	slot.StagedNVR = "quay-operator-fbc-3.18.2-20261009000000.ocp4.16"
	if got, _ := Staged(tags, "quay-operator", "3.18.2", slot); got.Name != slot.StagedNVR {
		t.Errorf("Staged = %q, want the NVR's tag %s", got.Name, slot.StagedNVR)
	}
	// The NVR's tag has another digest: it does not win.
	slot.StagedNVR = newQO16
	if got, _ := Staged(tags, "quay-operator", "3.18.2", slot); got.Name != oldQO16 {
		t.Errorf("Staged = %q, want %s: the NVR's tag has another digest", got.Name, oldQO16)
	}
	slot.Staged = digest("f")
	if got, ok := Staged(tags, "quay-operator", "3.18.2", slot); ok {
		t.Errorf("Staged = %q for a digest no tag has, want none", got.Name)
	}
	slot.Staged = ""
	if _, ok := Staged(tags, "quay-operator", "3.18.2", slot); ok {
		t.Error("Staged found a tag for no digest")
	}
}

func tag(operator, ocp, ts string, d string) Tag {
	return Tag{Name: operator + "-fbc-3.18.2-" + ts + ".ocp" + ocp, Digest: digest(d), Start: 1}
}

// byOperator groups tags by their operator, as Resolve takes them.
func byOperator(tags ...Tag) map[string][]Tag {
	m := map[string][]Tag{}
	for _, t := range tags {
		op := t.Name[:strings.Index(t.Name, "-fbc-")]
		m[op] = append(m[op], t)
	}
	return m
}

func matches(r Result) []string {
	var out []string
	for _, t := range r.Tags {
		out = append(out, fmt.Sprintf("%s/%s=%s", t.Operator, t.OCP, t.Basis))
	}
	return out
}

// A slot with a staged digest gets the tag of that digest, in slot order,
// whether or not ART has built again: the newer build is information, naming
// its tag, and never the tag used.
func TestResolveStaged(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14", Staged: digest("3"), StagedNVR: "quay-operator-fbc-3.18.2-20261015132257.ocp4.14"},
		{Operator: "quay-operator", OCP: "4.16", Staged: digest("9"), StagedNVR: oldQO16},
	}
	tags := byOperator(
		tag("quay-operator", "4.14", "20261015132257", "3"),
		Tag{Name: newQO16, Digest: digest("2"), Start: 20},
		Tag{Name: oldQO16, Digest: digest("9"), Start: 10},
	)
	got := Resolve(slots, "3.18.2", tags)
	wantSpecs := []string{Repository + ":quay-operator-fbc-3.18.2-20261015132257.ocp4.14", Repository + ":" + oldQO16}
	if !slices.Equal(got.Pullspecs, wantSpecs) {
		t.Errorf("Pullspecs = %v, want %v", got.Pullspecs, wantSpecs)
	}
	if len(got.Errors) != 0 || len(got.Warnings) != 0 {
		t.Errorf("Errors = %q, Warnings = %q; want none for staged slots", got.Errors, got.Warnings)
	}
	wantInfo := []string{"quay-operator OCP 4.16: ART has a newer build: " + newQO16}
	if !slices.Equal(got.Info, wantInfo) {
		t.Errorf("Info = %q, want %q", got.Info, wantInfo)
	}
	if want := []string{"quay-operator/4.14=staged", "quay-operator/4.16=staged"}; !slices.Equal(matches(got), want) {
		t.Errorf("bases = %v, want %v", matches(got), want)
	}
	if got.Tags[0].NewerTag != "" || got.Tags[1].NewerTag != newQO16 || got.Tags[1].Digest != digest("9") {
		t.Errorf("tags = %+v, want the staged tag of 4.16 with the newer tag named, none for 4.14", got.Tags)
	}
}

// With no tag of the staged digest the slot is an error naming the operator,
// the OCP version and the staged digest, the newest tag is not taken in its
// place, and the pullspecs are empty: a partial list must not be released.
func TestResolveStagedMissing(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14", Staged: digest("3")},
		{Operator: "quay-operator", OCP: "4.16", Staged: digest("7")},
	}
	tags := byOperator(
		tag("quay-operator", "4.14", "20261015132257", "3"),
		Tag{Name: newQO16, Digest: digest("2"), Start: 20},
	)
	got := Resolve(slots, "3.18.2", tags)
	wantErr := []string{"quay-operator OCP 4.16: no tag in " + Repository + " points at the staged digest " + digest("7") + " (searched 2 tags)"}
	if !slices.Equal(got.Errors, wantErr) {
		t.Errorf("Errors = %q, want %q", got.Errors, wantErr)
	}
	if len(got.Pullspecs) != 0 {
		t.Errorf("Pullspecs = %v, want none with errors", got.Pullspecs)
	}
	if len(got.Tags) != 1 || got.Tags[0].OCP != "4.14" {
		t.Errorf("Tags = %+v, want only the 4.14 slot: the newest 4.16 tag is not a substitute", got.Tags)
	}
}

// A slot with no staged candidate takes the newest tag and says so in a
// warning, whatever the other slots hold.
func TestResolveUnstaged(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14", Staged: digest("3")},
		{Operator: "quay-operator", OCP: "4.16"},
		{Operator: "quay-bridge-operator", OCP: "4.14"},
	}
	tags := byOperator(
		tag("quay-operator", "4.14", "20261015132257", "3"),
		Tag{Name: oldQO16, Digest: digest("9"), Start: 10},
		Tag{Name: newQO16, Digest: digest("2"), Start: 20},
		tag("quay-bridge-operator", "4.14", "20261012232756", "8"),
	)
	got := Resolve(slots, "3.18.2", tags)
	wantSpecs := []string{
		Repository + ":quay-operator-fbc-3.18.2-20261015132257.ocp4.14",
		Repository + ":" + newQO16,
		Repository + ":quay-bridge-operator-fbc-3.18.2-20261012232756.ocp4.14",
	}
	if !slices.Equal(got.Pullspecs, wantSpecs) {
		t.Errorf("Pullspecs = %v, want %v", got.Pullspecs, wantSpecs)
	}
	wantWarn := []string{
		"quay-operator OCP 4.16: newest ART build, not staged in RR (" + newQO16 + ")",
		"quay-bridge-operator OCP 4.14: newest ART build, not staged in RR (quay-bridge-operator-fbc-3.18.2-20261012232756.ocp4.14)",
	}
	if !slices.Equal(got.Warnings, wantWarn) {
		t.Errorf("Warnings = %q, want %q", got.Warnings, wantWarn)
	}
	if len(got.Errors) != 0 || len(got.Info) != 0 {
		t.Errorf("Errors = %q, Info = %q; want none", got.Errors, got.Info)
	}
	if want := []string{"quay-operator/4.14=staged", "quay-operator/4.16=newest", "quay-bridge-operator/4.14=newest"}; !slices.Equal(matches(got), want) {
		t.Errorf("bases = %v, want %v", matches(got), want)
	}
}

// A slot with no staged candidate and no tag is an error naming the operator
// and OCP version, as before.
func TestResolveUnstagedMissing(t *testing.T) {
	slots := []Slot{
		{Operator: "quay-operator", OCP: "4.14"},
		{Operator: "quay-operator", OCP: "4.16"},
		{Operator: "container-security-operator", OCP: "4.14"},
	}
	got := Resolve(slots, "3.18.2", byOperator(tag("quay-operator", "4.14", "20261015132257", "3")))
	wantErr := []string{
		"quay-operator OCP 4.16: no tag in " + Repository,
		"container-security-operator OCP 4.14: no tag in " + Repository,
	}
	if !slices.Equal(got.Errors, wantErr) {
		t.Errorf("Errors = %q, want %q", got.Errors, wantErr)
	}
	if len(got.Pullspecs) != 0 || len(got.Tags) != 1 {
		t.Errorf("Pullspecs = %v, Tags = %+v; want no pullspecs, the 1 found", got.Pullspecs, got.Tags)
	}
}

// The staged NVR that names a tag is used only when that tag's digest is the
// staged one: otherwise the digest decides, and the NVR's tag is the newer
// build, not the answer.
func TestResolveStagedNVR(t *testing.T) {
	tags := byOperator(
		Tag{Name: newQO16, Digest: digest("2"), Start: 20},
		Tag{Name: oldQO16, Digest: digest("9"), Start: 10},
	)
	for name, tc := range map[string]struct {
		nvr, want string
	}{
		"nvr names the staged tag":       {oldQO16, oldQO16},
		"nvr names another digest's tag": {newQO16, oldQO16},
		"nvr is no tag":                  {"quay-operator-fbc-3.18.2-20261001000000.ocp4.16", oldQO16},
		"no nvr":                         {"", oldQO16},
	} {
		got := Resolve([]Slot{{Operator: "quay-operator", OCP: "4.16", Staged: digest("9"), StagedNVR: tc.nvr}}, "3.18.2", tags)
		if len(got.Tags) != 1 || got.Tags[0].Tag != tc.want || len(got.Errors) != 0 {
			t.Errorf("%s: Tags = %+v, Errors = %q; want %s", name, got.Tags, got.Errors, tc.want)
		}
	}
	// An NVR naming a tag whose digest is not the staged one, with no tag of
	// the staged digest, is an error: the NVR is not enough.
	got := Resolve([]Slot{{Operator: "quay-operator", OCP: "4.16", Staged: digest("7"), StagedNVR: newQO16}}, "3.18.2", tags)
	if len(got.Errors) != 1 || len(got.Tags) != 0 {
		t.Errorf("Errors = %q, Tags = %+v; want one error and no tag", got.Errors, got.Tags)
	}
}

func TestResolveNoSlots(t *testing.T) {
	got := Resolve(nil, "3.18.2", nil)
	if len(got.Errors) != 1 || len(got.Pullspecs) != 0 {
		t.Errorf("Resolve(nil) = %+v, want one error and no pullspecs", got)
	}
}
