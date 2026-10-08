package catalog

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const repoPrefix = "/repositories/registry/registry.access.redhat.com/repository/"

// omrPage has one unpublished image, which must not count as shipped.
const omrPage = `{"total":2,"data":[
	{"repositories":[{"published":true,"tags":[{"name":"2.0.12"}]}]},
	{"repositories":[{"published":false,"tags":[{"name":"2.0.13"}]}]}]}`

// catalogServer answers the quay repositories with the recorded quay-rhel9
// pages and OMR with omrPage. Setting *fail makes every request a 503.
func catalogServer(t *testing.T, fail *bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, repoPrefix), "/images")
		switch {
		case repo == "openshift/mirror-registry-rhel8":
			_, _ = w.Write([]byte(omrPage))
		case strings.HasPrefix(repo, "quay/"):
			http.ServeFile(w, r, "testdata/quay-rhel9-page"+r.URL.Query().Get("page")+".json")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPublishedTagsFollowsPages(t *testing.T) {
	fail := false
	srv := catalogServer(t, &fail)
	tags, err := NewClient(srv.URL+"/", srv.Client()).PublishedTags(t.Context(), "quay/quay-rhel9")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"v3.18.0", "v3.17.5"} { // pages 0 and 1
		if !slices.Contains(tags, want) {
			t.Errorf("tags missing %s", want)
		}
	}
}

func TestShipped(t *testing.T) {
	fail := false
	srv := catalogServer(t, &fail)
	s := NewShipped(NewClient(srv.URL, srv.Client()), slog.Default())
	s.Refresh(t.Context())

	for _, tc := range []struct {
		product, version string
		want             bool
	}{
		{"quay", "3.17.5", true},
		{"quay", "3.17.6", false},
		{"quay", "3.18.0", true},
		{"omr", "2.0.12", true},  // bare tag
		{"omr", "2.0.13", false}, // unpublished
		{"omr", "3.17.5", false}, // quay tag, other product
	} {
		if got := s.Has(tc.product, tc.version); got != tc.want {
			t.Errorf("Has(%s, %s) = %v, want %v", tc.product, tc.version, got, tc.want)
		}
	}

	fail = true
	s.Refresh(t.Context())
	if !s.Has("quay", "3.17.5") {
		t.Error("failed refresh dropped the last good set")
	}

	var disabled *Shipped
	if disabled.Has("quay", "3.17.5") {
		t.Error("nil Shipped reports a version published")
	}
}
