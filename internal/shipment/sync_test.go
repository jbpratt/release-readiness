package shipment

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/quay/release-readiness/internal/model"
)

const mrs = "/api/v4/projects/116177/merge_requests"

// routes serves the recorded fixtures by request path, and a search by its
// term; anything else is 404.
func routes(t *testing.T) map[string][]byte {
	t.Helper()
	r := map[string][]byte{}
	for p, f := range map[string]string{
		"/raw/quay-3.17/releases.yml": "releases-quay-3.17.yml",
		"/raw/quay-3.18/releases.yml": "releases-quay-3.18.yml",
		mrs + "/948":                  "mr-948.json",
		mrs + "/956":                  "mr-956.json",
		mrs + "/957/changes":          "changes-957.json",
		mrs + "/975/changes":          "changes-975.json",
		mrs + "/974/changes":          "changes-974.json",
		mrs + "?3.17.6":               "search-3.17.6.json",
		mrs + "?5.0.0":                "search-5.0.0.json",
	} {
		data, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		r[p] = data
	}
	return r
}

type fakeStore map[string]*model.ShipmentMR

func (s fakeStore) UpsertShipmentMR(_ context.Context, mr *model.ShipmentMR) error {
	s[mr.Version] = mr
	return nil
}

// runSync syncs the version against routes and returns what was stored and the
// requests made.
func runSync(t *testing.T, routes map[string][]byte, token, version string) (fakeStore, []*http.Request) {
	t.Helper()
	var reqs []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		key := r.URL.Path
		if q := r.URL.Query(); q.Has("search") {
			if q.Get("state") != "all" || q.Get("order_by") != "updated_at" || q.Get("per_page") != "100" {
				t.Errorf("search query %s", r.URL.RawQuery)
			}
			key += "?" + q.Get("search")
		}
		data, ok := routes[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	store := fakeStore{}
	s := NewSyncer(NewClient(srv.URL, srv.URL+"/raw", 116177, token, srv.Client()), store, nil, slog.New(slog.DiscardHandler))
	if err := s.syncVersion(t.Context(), version); err != nil {
		t.Fatal(err)
	}
	return store, reqs
}

func paths(reqs []*http.Request) []string {
	var p []string
	for _, r := range reqs {
		p = append(p, r.URL.Path)
	}
	return p
}

func TestSyncPointer(t *testing.T) {
	store, reqs := runSync(t, routes(t), "", "quay-v3.18.1")
	mr := store["quay-v3.18.1"]
	if mr == nil {
		t.Fatalf("nothing stored; requests %v", paths(reqs))
	}
	Derive(mr)
	if mr.IID != 948 || mr.Source != "pointer" || mr.Assembly != "3.18.1" || mr.Stage != "shipped" || mr.MergedAt == nil || mr.HeadPipelineStatus != "success" {
		t.Errorf("stored %+v", mr)
	}
	if len(reqs) != 2 {
		t.Errorf("requests %v, want releases.yml and the MR", paths(reqs))
	}
}

func TestSyncClosedPointerFallsBackToOpenMR(t *testing.T) {
	store, reqs := runSync(t, routes(t), "", "quay-v3.17.6")
	mr := store["quay-v3.17.6"]
	if mr == nil {
		t.Fatalf("nothing stored; requests %v", paths(reqs))
	}
	Derive(mr)
	if mr.IID != 957 || mr.Source != "search" || mr.State != "opened" || mr.Stage != "open" || !mr.PipelineFailed || mr.NeedsApproval {
		t.Errorf("stored %+v", mr)
	}
}

func TestSyncClosedPointerKeptWhenSearchFindsNone(t *testing.T) {
	r := routes(t)
	r[mrs+"?3.17.6"] = []byte("[]")
	store, reqs := runSync(t, r, "", "quay-v3.17.6")
	mr := store["quay-v3.17.6"]
	if mr == nil {
		t.Fatalf("nothing stored; requests %v", paths(reqs))
	}
	Derive(mr)
	if mr.IID != 956 || mr.Source != "pointer" || mr.Stage != "closed" || mr.PipelineFailed {
		t.Errorf("stored %+v", mr)
	}
}

func TestSyncNoPointerSearches(t *testing.T) {
	// quay-5.0 is not served: the branch is missing. The open OCP
	// 5.0.0-rc.6 MRs are on other branches, and the closed !974 is never
	// read once the open !975 is found.
	store, reqs := runSync(t, routes(t), "", "quay-v5.0.0")
	mr := store["quay-v5.0.0"]
	if mr == nil {
		t.Fatalf("nothing stored; requests %v", paths(reqs))
	}
	Derive(mr)
	if mr.IID != 975 || mr.Source != "search" || !mr.NeedsApproval || !mr.PipelineFailed {
		t.Errorf("stored %+v", mr)
	}
	want := []string{"/raw/quay-5.0/releases.yml", mrs, mrs + "/975/changes"}
	if got := paths(reqs); !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v", got, want)
	}
}

func TestSyncSearchRejectsNonQuayChanges(t *testing.T) {
	// Without !975, the only MR on a prepare-shipment-5.0.0- branch is !974,
	// whose files are not under shipment/quay/.
	r := routes(t)
	var found []map[string]any
	if err := json.Unmarshal(r[mrs+"?5.0.0"], &found); err != nil {
		t.Fatal(err)
	}
	found = slices.DeleteFunc(found, func(m map[string]any) bool { return m["iid"] == 975.0 })
	r[mrs+"?5.0.0"], _ = json.Marshal(found)

	store, reqs := runSync(t, r, "", "quay-v5.0.0")
	if mr := store["quay-v5.0.0"]; mr != nil {
		t.Errorf("stored %+v, want none", mr)
	}
	if !slices.Contains(paths(reqs), mrs+"/974/changes") {
		t.Errorf("requests %v, want !974's changes read", paths(reqs))
	}
}

func TestToken(t *testing.T) {
	// 3.17.6 reads releases.yml, the MR, the search and changes.
	for _, token := range []string{"", "s3cret"} {
		_, reqs := runSync(t, routes(t), token, "quay-v3.17.6")
		for _, r := range reqs {
			var want []string
			if token != "" && r.URL.Path != "/raw/quay-3.17/releases.yml" {
				want = []string{token}
			}
			if got := r.Header.Values("PRIVATE-TOKEN"); !slices.Equal(got, want) {
				t.Errorf("token %q: %s sent PRIVATE-TOKEN %q, want %q", token, r.URL.Path, got, want)
			}
		}
	}
}

func TestDerive(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		mr                         model.ShipmentMR
		stage                      string
		pipelineFailed, needsAppvl bool
	}{
		{"merged", model.ShipmentMR{State: "merged", Labels: []string{"prod-release-success", "stage-release-success"}}, "shipped", false, false},
		{"closed", model.ShipmentMR{State: "closed", Labels: []string{"stage-release-success"}, HeadPipelineStatus: "failed", DetailedMergeStatus: "not_approved"}, "closed", false, false},
		{"prod released", model.ShipmentMR{State: "opened", Labels: []string{"stage-release-success", "prod-release-success"}}, "prod_released", false, false},
		{"stage released", model.ShipmentMR{State: "opened", Labels: []string{"stage-release-success"}}, "stage_released", false, false},
		{"open", model.ShipmentMR{State: "opened"}, "open", false, false},
		{"pipeline failed", model.ShipmentMR{State: "opened", HeadPipelineStatus: "failed"}, "open", true, false},
		{"needs approval", model.ShipmentMR{State: "opened", DetailedMergeStatus: "not_approved"}, "open", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Derive(&tc.mr)
			if tc.mr.Stage != tc.stage || tc.mr.PipelineFailed != tc.pipelineFailed || tc.mr.NeedsApproval != tc.needsAppvl {
				t.Errorf("got stage %q, pipeline failed %v, needs approval %v", tc.mr.Stage, tc.mr.PipelineFailed, tc.mr.NeedsApproval)
			}
		})
	}
}
