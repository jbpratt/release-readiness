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
		var rows []string
		for name, digest := range tags {
			if strings.Contains(name, sub) {
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

// seed316 stores the stream's 3.16.7 FBCs, which make a complete candidate:
// quay-operator for OCP 4.12 and 4.13, the others for 4.12.
func seed316(t *testing.T, srv *Server) {
	t.Helper()
	const head = "quay-operator-metadata-container-3.16.7.202610121915"
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012201007.ocp4.12", head)
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012200000.ocp4.13", head)
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12", "qbo-bundle")
	getFBC(t, srv, "quay-v3.16.7", "quay-3-16")
}

type fbcTagsResponse struct {
	Repository string   `json:"repository"`
	Source     string   `json:"source"`
	Pullspecs  []string `json:"pullspecs"`
	Tags       []struct {
		Operator, OCP, Tag, Pullspec, Digest, Match string
		StagedDigest                                string `json:"staged_digest"`
	} `json:"tags"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// The candidate's slots are what the tags are looked up for, by operator,
// then OCP version, the newest tag of each; a tag whose digest is not the
// staged candidate's is a warning that still shows its pullspec.
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
	if keys := slices.Sorted(maps.Keys(raw)); !slices.Equal(keys, []string{"errors", "pullspecs", "repository", "source", "tags", "warnings"}) {
		t.Errorf("response keys %v", keys)
	}
	var rawTags []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tags"], &rawTags); err != nil || len(rawTags) == 0 {
		t.Fatalf("tags %s: %v", raw["tags"], err)
	}
	if keys := slices.Sorted(maps.Keys(rawTags[0])); !slices.Equal(keys, []string{"digest", "match", "ocp", "operator", "pullspec", "staged_digest", "staged_nvr", "tag"}) {
		t.Errorf("tag keys %v", keys)
	}

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &got)
	if got.Repository != strings.TrimSuffix(fbcTagRepo, ":") || got.Source != "candidate" {
		t.Errorf("repository %q, source %q; want the art-fbc repository, candidate", got.Repository, got.Source)
	}
	want := []string{fbcTagRepo + qo12, fbcTagRepo + qo13new, fbcTagRepo + cso, fbcTagRepo + qbo}
	if !slices.Equal(got.Pullspecs, want) {
		t.Errorf("pullspecs %v, want %v", got.Pullspecs, want)
	}
	if len(got.Errors) != 0 {
		t.Errorf("errors %v, want none", got.Errors)
	}
	var rows []string
	for _, tg := range got.Tags {
		rows = append(rows, tg.Operator+" "+tg.OCP+" "+tg.Tag+" "+tg.Match)
	}
	wantRows := []string{
		"quay-operator 4.12 " + qo12 + " match",
		"quay-operator 4.13 " + qo13new + " mismatch",
		"container-security-operator 4.12 " + cso + " match",
		"quay-bridge-operator 4.12 " + qbo + " match",
	}
	if !slices.Equal(rows, wantRows) {
		t.Errorf("tags:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(wantRows, "\n"))
	}
	wantWarn := fmt.Sprintf("quay-operator OCP 4.13: ART has a newer build than the staged candidate: tag %s is %s, the staged candidate %s is %s",
		qo13new, testDigest("art newer"), qo13, testDigest("fbc/"+qo13))
	if len(got.Warnings) != 1 || got.Warnings[0] != wantWarn {
		t.Errorf("warnings %q, want [%q]", got.Warnings, wantWarn)
	}
}

// A slot ART has no tag for is an error naming it, and no pullspecs are
// offered; the tags found are still shown.
func TestGetFBCTagsMissing(t *testing.T) {
	srv := setupTestServer(t)
	seed316(t, srv)
	qo12 := "quay-operator-fbc-3.16.7-20261012201007.ocp4.12"
	srv.ArtFBC = fakeArtFBC(t, map[string]string{qo12: testDigest("fbc/" + qo12)})

	var got fbcTagsResponse
	getJSON(t, srv, "/api/v1/releases/quay-v3.16.7/fbc-tags", http.StatusOK, &got)
	wantErr := []string{
		"quay-operator OCP 4.13: no tag in " + strings.TrimSuffix(fbcTagRepo, ":"),
		"container-security-operator OCP 4.12: no tag in " + strings.TrimSuffix(fbcTagRepo, ":"),
		"quay-bridge-operator OCP 4.12: no tag in " + strings.TrimSuffix(fbcTagRepo, ":"),
	}
	if !slices.Equal(got.Errors, wantErr) {
		t.Errorf("errors %q, want %q", got.Errors, wantErr)
	}
	if len(got.Pullspecs) != 0 || len(got.Tags) != 1 {
		t.Errorf("pullspecs %v, tags %d; want none, the 1 found", got.Pullspecs, len(got.Tags))
	}
}

// With no candidate the slots are every operator at each of the stream's
// OCP_TARGET_VERSIONS, and the tags are not checked against a staged digest.
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
	if got.Source != "ocp-build-data" || len(got.Tags) != 32 || len(got.Warnings) != 0 {
		t.Errorf("source %q, %d tags, warnings %q; want ocp-build-data, 32 tags, none", got.Source, len(got.Tags), got.Warnings)
	}
	if want := []string{"quay-bridge-operator OCP 5.0: no tag in " + strings.TrimSuffix(fbcTagRepo, ":")}; !slices.Equal(got.Errors, want) {
		t.Errorf("errors %q, want %q", got.Errors, want)
	}
	for _, tg := range got.Tags {
		if tg.Match != "unknown" {
			t.Errorf("%s %s: match %q, want unknown without a staged digest", tg.Operator, tg.OCP, tg.Match)
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
