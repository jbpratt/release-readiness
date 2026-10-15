package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/quay/release-readiness/internal/artfbc"
	"github.com/quay/release-readiness/internal/model"
)

const fbcTagAPI = "/api/v1/repository/redhat-user-workloads/ocp-art-tenant/art-fbc/tag/"

// fakeArtFBC serves tags like quay.io's tag API, name to digest, newest
// first, and the stream 3.18's group.yml. It fails the test on any other
// request.
func fakeArtFBC(t *testing.T, tags map[string]string) *artfbc.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(fbcTagAPI, func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Query().Get("filter_tag_name"), "like:")
		specific := r.URL.Query().Get("specificTag")
		var rows []string
		for name, digest := range tags {
			if specific != "" && name == specific || specific == "" && strings.Contains(name, sub) {
				rows = append(rows, fmt.Sprintf(`{"name":%q,"manifest_digest":%q,"start_ts":%d}`, name, digest, 1000+len(rows)))
			}
		}
		slices.Sort(rows)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"tags":[%s],"page":1,"has_additional":false}`, strings.Join(rows, ","))
	})
	mux.HandleFunc("/quay-3.18/group.yml", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("../artfbc/testdata/group.yml")
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return artfbc.NewClient(ts.URL, ts.URL, ts.Client())
}

const fbcTagRepo = "quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc:"

// seed316Stream stores the stream's 3.16.7 FBCs, which make a complete
// candidate: quay-operator for OCP 4.12 and 4.13, the others for 4.12. It
// returns their pullspecs, in that order.
func seed316Stream(t *testing.T, srv *Server) []string {
	t.Helper()
	const head = "quay-operator-metadata-container-3.16.7.202610121915"
	return []string{
		seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012201007.ocp4.12", head),
		seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012200000.ocp4.13", head),
		seedHistoryFBC(t, srv, "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12", "cso-bundle"),
		seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12", "qbo-bundle"),
	}
}

// seed316 is seed316Stream with those FBCs also staged for stage in one FBC
// Snapshot, so that RR's candidate is what ART staged.
func seed316(t *testing.T, srv *Server) {
	t.Helper()
	images := seed316Stream(t, srv)
	seedStagedFBC(t, srv, "fbc-quay-3-16", "3.16.7", "quay-stage-3-16-7-fbc-20261012210000", "2026-10-12T21:00:00Z", images, "Succeeded")
	if cand, _ := getFBC(t, srv, "quay-v3.16.7", "quay-3-16"); cand.Source != "stage_snapshots" {
		t.Fatalf("candidate source %q, want stage_snapshots", cand.Source)
	}
}

type fbcTagsResponse struct {
	Repository string   `json:"repository"`
	Source     string   `json:"source"`
	Pullspecs  []string `json:"pullspecs"`
	Tags       []struct {
		Operator, OCP, Tag, Pullspec, Digest, Basis string
		StagedDigest                                string `json:"staged_digest"`
		NewerTag                                    string `json:"newer_tag"`
	} `json:"tags"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	Info     []string `json:"info"`
}

// The candidate's slots are what the tags are looked up for, by operator,
// then OCP version, the tag of each pointing at the digest RR staged: ART's
// newer build of a slot is information, and is not the tag listed.
func TestGetFBCTags(t *testing.T) {
	srv := setupTestServer(t)
	seed316(t, srv)
	qo12 := "quay-operator-fbc-3.16.7-20261012201007.ocp4.12"
	qo13 := "quay-operator-fbc-3.16.7-20261012200000.ocp4.13"
	qo13new := "quay-operator-fbc-3.16.7-20261013100000.ocp4.13"
	cso := "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12"
	qbo := "quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12"
	srv.ArtFBC = fakeArtFBC(t, map[string]string{
		qo12:                       testDigest("fbc/" + qo12),
		qo13:                       testDigest("fbc/" + qo13),
		qo13new:                    testDigest("art newer"),
		qo13 + "-linux-s390x":      testDigest("arch"),
		cso:                        testDigest("fbc/" + cso),
		qbo:                        testDigest("fbc/" + qbo),
		"quay-operator-fbc-3.16.7": testDigest("base"),
	})

	// The web reads these exact keys.
	var raw map[string]json.RawMessage
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &raw)
	if keys := slices.Sorted(maps.Keys(raw)); !slices.Equal(keys, []string{"errors", "info", "pullspecs", "repository", "source", "tags", "warnings"}) {
		t.Errorf("response keys %v", keys)
	}
	var rawTags []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tags"], &rawTags); err != nil || len(rawTags) == 0 {
		t.Fatalf("tags %s: %v", raw["tags"], err)
	}
	if keys := slices.Sorted(maps.Keys(rawTags[0])); !slices.Equal(keys, []string{"basis", "digest", "newer_tag", "ocp", "operator", "pullspec", "staged_digest", "staged_nvr", "tag"}) {
		t.Errorf("tag keys %v", keys)
	}

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &got)
	if got.Repository != strings.TrimSuffix(fbcTagRepo, ":") || got.Source != "candidate" {
		t.Errorf("repository %q, source %q; want the art-fbc repository, candidate", got.Repository, got.Source)
	}
	want := []string{fbcTagRepo + qo12, fbcTagRepo + qo13, fbcTagRepo + cso, fbcTagRepo + qbo}
	if !slices.Equal(got.Pullspecs, want) {
		t.Errorf("pullspecs %v, want %v", got.Pullspecs, want)
	}
	if len(got.Errors) != 0 || len(got.Warnings) != 0 {
		t.Errorf("errors %v, warnings %v; want none: every slot is staged", got.Errors, got.Warnings)
	}
	var rows []string
	for _, tg := range got.Tags {
		rows = append(rows, tg.Operator+" "+tg.OCP+" "+tg.Tag+" "+tg.Basis+" "+tg.NewerTag)
		if tg.StagedDigest != tg.Digest {
			t.Errorf("%s %s: tag digest %s, staged %s; want them equal", tg.Operator, tg.OCP, tg.Digest, tg.StagedDigest)
		}
	}
	wantRows := []string{
		"quay-operator 4.12 " + qo12 + " staged ",
		"quay-operator 4.13 " + qo13 + " staged " + qo13new,
		"container-security-operator 4.12 " + cso + " staged ",
		"quay-bridge-operator 4.12 " + qbo + " staged ",
	}
	if !slices.Equal(rows, wantRows) {
		t.Errorf("tags:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(wantRows, "\n"))
	}
	if want := []string{"quay-operator OCP 4.13: ART has a newer build: " + qo13new}; !slices.Equal(got.Info, want) {
		t.Errorf("info %q, want %q", got.Info, want)
	}
}

// A staged digest no ART tag points at is an error naming the slot, the
// operator, the OCP version and the digest, whatever newest tag ART has for
// it, and no pullspecs are offered; the tags found are still shown.
func TestGetFBCTagsMissing(t *testing.T) {
	srv := setupTestServer(t)
	seed316(t, srv)
	qo12 := "quay-operator-fbc-3.16.7-20261012201007.ocp4.12"
	qo13 := "quay-operator-fbc-3.16.7-20261012200000.ocp4.13"
	cso := "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12"
	srv.ArtFBC = fakeArtFBC(t, map[string]string{
		qo12: testDigest("fbc/" + qo12),
		// ART's only OCP 4.13 tag is another build than the staged one.
		qo13: testDigest("not staged"),
	})

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &got)
	repo := strings.TrimSuffix(fbcTagRepo, ":")
	wantErr := []string{
		"quay-operator OCP 4.13: no tag in " + repo + " points at the staged digest " + testDigest("fbc/"+qo13) + " (searched 2 tags)",
		"container-security-operator OCP 4.12: no tag in " + repo + " points at the staged digest " + testDigest("fbc/"+cso) + " (searched 0 tags)",
		"quay-bridge-operator OCP 4.12: no tag in " + repo + " points at the staged digest " + testDigest("fbc/quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12") + " (searched 0 tags)",
	}
	if !slices.Equal(got.Errors, wantErr) {
		t.Errorf("errors:\n%s\nwant:\n%s", strings.Join(got.Errors, "\n"), strings.Join(wantErr, "\n"))
	}
	if len(got.Pullspecs) != 0 || len(got.Tags) != 1 {
		t.Errorf("pullspecs %v, tags %d; want none, the 1 found", got.Pullspecs, len(got.Tags))
	}
}

// A candidate of what the stream built, with nothing staged, has no staged
// digest to tie a tag to: each slot takes the newest tag, with a warning.
func TestGetFBCTagsStreamCandidate(t *testing.T) {
	srv := setupTestServer(t)
	seed316Stream(t, srv)
	if cand, _ := getFBC(t, srv, "quay-v3.16.7", "quay-3-16"); cand.Source != "stream" {
		t.Fatalf("candidate source %q, want stream", cand.Source)
	}
	qo12 := "quay-operator-fbc-3.16.7-20261012201007.ocp4.12"
	qo13 := "quay-operator-fbc-3.16.7-20261012200000.ocp4.13"
	qo13new := "quay-operator-fbc-3.16.7-20261013100000.ocp4.13"
	cso := "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12"
	qbo := "quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12"
	srv.ArtFBC = fakeArtFBC(t, map[string]string{
		qo12: testDigest("fbc/" + qo12), qo13: testDigest("fbc/" + qo13), qo13new: testDigest("art newer"),
		cso: testDigest("fbc/" + cso), qbo: testDigest("fbc/" + qbo),
	})

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &got)
	want := []string{fbcTagRepo + qo12, fbcTagRepo + qo13new, fbcTagRepo + cso, fbcTagRepo + qbo}
	if got.Source != "candidate" || !slices.Equal(got.Pullspecs, want) || len(got.Errors) != 0 || len(got.Info) != 0 {
		t.Errorf("source %q, pullspecs %v, errors %v, info %v; want candidate, %v, none", got.Source, got.Pullspecs, got.Errors, got.Info, want)
	}
	wantWarn := []string{
		"quay-operator OCP 4.12: newest ART build, not staged in RR (" + qo12 + ")",
		"quay-operator OCP 4.13: newest ART build, not staged in RR (" + qo13new + ")",
		"container-security-operator OCP 4.12: newest ART build, not staged in RR (" + cso + ")",
		"quay-bridge-operator OCP 4.12: newest ART build, not staged in RR (" + qbo + ")",
	}
	if !slices.Equal(got.Warnings, wantWarn) {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(got.Warnings, "\n"), strings.Join(wantWarn, "\n"))
	}
	for _, tg := range got.Tags {
		if tg.Basis != "newest" || tg.StagedDigest != "" {
			t.Errorf("%s %s: basis %q, staged digest %q; want newest, none", tg.Operator, tg.OCP, tg.Basis, tg.StagedDigest)
		}
	}
}

// With no candidate the slots are every operator at each of the stream's
// OCP_TARGET_VERSIONS, and nothing is staged: each takes the newest tag, with
// a warning.
func TestGetFBCTagsNoCandidate(t *testing.T) {
	srv := setupTestServer(t)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.2", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for _, op := range []string{"quay-operator", "container-security-operator", "quay-bridge-operator"} {
		for _, ocp := range []string{"4.12", "4.14", "4.15", "4.16", "4.17", "4.18", "4.19", "4.20", "4.21", "4.22", "5.0"} {
			tags[op+"-fbc-3.18.2-20261015132328.ocp"+ocp] = testDigest(op + ocp)
		}
	}
	delete(tags, "quay-bridge-operator-fbc-3.18.2-20261015132328.ocp5.0")
	srv.ArtFBC = fakeArtFBC(t, tags)

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/fbc-tags", http.StatusOK, &got)
	if got.Source != "ocp-build-data" || len(got.Tags) != 32 || len(got.Warnings) != 32 {
		t.Errorf("source %q, %d tags, %d warnings; want ocp-build-data, 32 tags, 32 warnings", got.Source, len(got.Tags), len(got.Warnings))
	}
	if want := []string{"quay-bridge-operator OCP 5.0: no tag in " + strings.TrimSuffix(fbcTagRepo, ":")}; !slices.Equal(got.Errors, want) {
		t.Errorf("errors %q, want %q", got.Errors, want)
	}
	for _, tg := range got.Tags {
		if tg.Basis != "newest" {
			t.Errorf("%s %s: basis %q, want newest without a staged digest", tg.Operator, tg.OCP, tg.Basis)
		}
	}
}

func TestGetFBCTagsErrors(t *testing.T) {
	srv := setupTestServer(t)
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusNotFound, nil)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.16.7", KonfluxApplication: "quay-3-16"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.16.z", KonfluxApplication: "quay-3-16"}); err != nil {
		t.Fatal(err)
	}
	// The lookup is off.
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusServiceUnavailable, nil)
	// Not a version ART builds.
	srv.ArtFBC = fakeArtFBC(t, nil)
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.z/fbc-tags", http.StatusNotFound, nil)
	// Quay fails.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer down.Close()
	srv.ArtFBC = artfbc.NewClient(down.URL, down.URL, down.Client())
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusBadGateway, nil)
}
