package scan

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	// digest is the image every test scans, ART record "rec".
	digest  = "sha256:8ee6f8cbca1a68a86f3b9bfbd250eea029ceebe340211a75b88f18b0d137e0b8"
	plrPage = "https://konflux-ui.example/ns/art-quay-tenant/applications/quay-3-18/pipelineruns/"
)

var (
	plrFilter = regexp.MustCompile(`^data_type == "tekton.dev/v1.PipelineRun" && data.metadata.name == "([^"]+)"$`)
	trFilter  = regexp.MustCompile(`^data_type == "tekton.dev/v1.TaskRun" && data.metadata.labels\["tekton.dev/pipelineRun"\] == "([^"]+)" && data.metadata.labels\["tekton.dev/pipelineTask"\] == "([^"]+)"$`)
)

// service fakes Tekton Results over the runs of testdata fixtures, and ART
// build history whose records link to pages[record_id].
type service struct {
	t                      *testing.T
	pipelineRuns, taskRuns []json.RawMessage
	pages                  map[string]string
	// status, when set, answers every Results request.
	status int
	// maxPage caps the records of a page.
	maxPage int
	// art and results count the requests to each.
	art, results int
}

func newService(t *testing.T, fixtures ...string) *service {
	t.Helper()
	svc := &service{t: t, pages: map[string]string{}, maxPage: 50}
	for _, f := range fixtures {
		b, err := os.ReadFile(filepath.Join("testdata", f+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			PipelineRun json.RawMessage   `json:"pipelineRun"`
			TaskRuns    []json.RawMessage `json:"taskRuns"`
		}
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatal(err)
		}
		svc.pipelineRuns = append(svc.pipelineRuns, fx.PipelineRun)
		svc.taskRuns = append(svc.taskRuns, fx.TaskRuns...)
	}
	return svc
}

func (svc *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch r.URL.Path {
	case "/build":
		svc.art++
		rec := map[string]string{"nvr": q.Get("nvr"), "record_id": q.Get("record_id")}
		if page, ok := svc.pages[q.Get("record_id")]; ok {
			rec["build_pipeline_url"] = page
		}
		_ = json.NewEncoder(w).Encode(rec)
	case "/apis/results.tekton.dev/v1alpha2/parents/art-quay-tenant/results/-/records":
		svc.results++
		if svc.status != 0 {
			http.Error(w, `{"code":16,"message":"unauthenticated"}`, svc.status)
			return
		}
		if n, err := strconv.Atoi(q.Get("page_size")); err != nil || n < 6 {
			http.Error(w, `{"code":3,"message":"invalid page size"}`, http.StatusBadRequest)
			return
		}
		typ, runs := svc.match(q.Get("filter"))
		start, _ := strconv.Atoi(q.Get("page_token"))
		end := min(start+svc.maxPage, len(runs))
		// The API omits empty fields: no records, no next page.
		var page struct {
			Records       []map[string]any `json:"records,omitempty"`
			NextPageToken string           `json:"next_page_token,omitempty"`
		}
		for _, run := range runs[start:end] {
			page.Records = append(page.Records, map[string]any{"data": map[string]any{"type": typ, "value": []byte(run)}})
		}
		if end < len(runs) {
			page.NextPageToken = strconv.Itoa(end)
		}
		_ = json.NewEncoder(w).Encode(page)
	default:
		svc.t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
	}
}

// match returns the data type and the runs that filter selects.
func (svc *service) match(filter string) (string, []json.RawMessage) {
	pick := func(runs []json.RawMessage, keep func(name string, labels map[string]string) bool) []json.RawMessage {
		var out []json.RawMessage
		for _, raw := range runs {
			var r struct {
				Metadata struct {
					Name   string            `json:"name"`
					Labels map[string]string `json:"labels"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &r); err != nil {
				svc.t.Fatal(err)
			}
			if keep(r.Metadata.Name, r.Metadata.Labels) {
				out = append(out, raw)
			}
		}
		return out
	}
	if m := plrFilter.FindStringSubmatch(filter); m != nil {
		return "tekton.dev/v1.PipelineRun", pick(svc.pipelineRuns, func(name string, _ map[string]string) bool { return name == m[1] })
	}
	if m := trFilter.FindStringSubmatch(filter); m != nil {
		return "tekton.dev/v1.TaskRun", pick(svc.taskRuns, func(_ string, l map[string]string) bool {
			return l["tekton.dev/pipelineRun"] == m[1] && l["tekton.dev/pipelineTask"] == m[2]
		})
	}
	svc.t.Errorf("unexpected filter %q", filter)
	return "", nil
}

type fakeStore struct {
	builds map[string]artbuild.Build
	scans  map[string]Scan
}

func (f *fakeStore) ResolvedArtBuilds(context.Context, []string) (map[string]artbuild.Build, error) {
	return f.builds, nil
}

func (f *fakeStore) ImageScans(context.Context, []string) (map[string]Scan, error) {
	return f.scans, nil
}

func (f *fakeStore) UpsertImageScan(_ context.Context, s Scan) error {
	f.scans[s.Digest] = s
	return nil
}

// newSyncer returns a Syncer of digest against svc, whose ART record links
// to the page of PipelineRun plr, or to none when plr is empty.
func newSyncer(t *testing.T, svc *service, plr string) (*Syncer, *fakeStore) {
	t.Helper()
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	if plr != "" {
		svc.pages["rec"] = plrPage + plr
	}
	store := &fakeStore{
		builds: map[string]artbuild.Build{digest: {Digest: digest, State: artbuild.StateResolved, NVR: "quay-operator-container-3.18.1", RecordID: "rec"}},
		scans:  map[string]Scan{},
	}
	images := func(context.Context) ([]string, error) { return []string{digest}, nil }
	s := NewSyncer(NewClient(srv.URL, srv.Client()), artbuild.NewClient(srv.URL, srv.Client()), store, images, slog.Default())
	s.gap = time.Millisecond
	return s, store
}

func counts(fixable, noFix model.SeverityCounts) *model.ScanCounts {
	return &model.ScanCounts{Basis: BasisArchFindings, Fixable: fixable, NoFix: noFix}
}

func TestSync(t *testing.T) {
	type sev = model.SeverityCounts
	for _, tc := range []struct {
		plr, state string
		counts     *model.ScanCounts
		reports    int
	}{
		// Each of the four matrix TaskRuns reports all four architectures.
		{"quay-3-18-quay-operator-7mbwk", StateScanned, counts(sev{}, sev{High: 24, Medium: 264, Low: 232}), 4},
		{"quay-3-18-quay-clair-ps9cc", StateScanned, counts(sev{}, sev{Critical: 44, High: 100, Medium: 320, Low: 236}), 4},
		// Tekton Succeeded=True, but TEST_OUTPUT ERROR and no SCAN_OUTPUT.
		{"quay-3-18-quay-quay-zwvxj", StateScanFailed, nil, 0},
		// Built before roxctl, with one Clair TaskRun per architecture instead.
		{"quay-3-18-quay-quay-d9s9v", StateNotScanned, nil, 0},
		{"quay-3-18-quay-operator-bundle-rjgp9", StateScanned, counts(sev{}, sev{Medium: 1}), 1},
		{"synth-no-scan-task", StateNotScanned, nil, 0},
		{"synth-running", StatePending, nil, 0},
		{"synth-taskruns-missing", StatePending, nil, 0},
		// The newest TaskRun that scanned, though listed last.
		{"synth-rox-newest", StateScanned, counts(sev{Critical: 1}, sev{Low: 5}), 1},
		// None scanned yet, but one TaskRun has not finished.
		{"synth-rox-running", StatePending, nil, 0},
	} {
		t.Run(tc.plr, func(t *testing.T) {
			svc := newService(t, tc.plr)
			s, store := newSyncer(t, svc, tc.plr)
			s.SyncOnce(t.Context())
			got, ok := store.scans[digest]
			if !ok {
				t.Fatal("no scan stored")
			}
			if got.State != tc.state || got.PipelineRun != tc.plr || got.DetailURL != plrPage+tc.plr ||
				!reflect.DeepEqual(got.Counts, tc.counts) || len(got.Reports) != tc.reports || (got.Reports == nil) != (tc.reports == 0) {
				t.Errorf("scan = %+v, counts %+v; want %s, counts %+v, %d reports", got, got.Counts, tc.state, tc.counts, tc.reports)
			}
			// The PipelineRun alone tells an image was not scanned.
			if tc.state == StateNotScanned && svc.results != 1 {
				t.Errorf("%d Results requests, want only the PipelineRun's", svc.results)
			}
		})
	}
}

// A Results error stores nothing and fails the pass; the next pass reads
// the scan, and a scanned image is not read again.
func TestSyncAfterError(t *testing.T) {
	svc := newService(t, "quay-3-18-quay-operator-7mbwk")
	s, store := newSyncer(t, svc, "quay-3-18-quay-operator-7mbwk")
	reg := syncstatus.New()
	s.Status = reg.Track("image-scans", 0)

	svc.status = http.StatusUnauthorized
	s.SyncOnce(t.Context())
	if p := reg.Problems(time.Now()); len(store.scans) != 0 || len(p) != 1 || !strings.Contains(p[0].Message, "401") {
		t.Fatalf("after a 401: stored %+v, problems %+v; want none stored and the 401 reported", store.scans, p)
	}

	svc.status = 0
	s.SyncOnce(t.Context())
	if p := reg.Problems(time.Now()); store.scans[digest].State != StateScanned || len(p) != 0 {
		t.Fatalf("after a 200: stored %+v, problems %+v; want scanned and no problem", store.scans, p)
	}
	requests := svc.art + svc.results
	s.SyncOnce(t.Context())
	if svc.art+svc.results != requests {
		t.Errorf("a scanned image was read again")
	}
}

// An ART record without a PipelineRun page is pending without asking
// Results, and not read again before retryAfter.
func TestSyncNoPipelineRunPage(t *testing.T) {
	svc := newService(t)
	s, store := newSyncer(t, svc, "")
	s.SyncOnce(t.Context())
	if got := store.scans[digest]; got.State != StatePending || got.DetailURL != "" || got.PipelineRun != "" || svc.results != 0 {
		t.Errorf("scan = %+v after %d Results requests; want pending, no page, none", got, svc.results)
	}
	s.SyncOnce(t.Context())
	if svc.art != 1 {
		t.Errorf("ART read %d times, want once", svc.art)
	}
}

// A pending scan is read again after retryAfter, from its stored PipelineRun.
func TestSyncRetriesPending(t *testing.T) {
	svc := newService(t, "synth-running")
	s, store := newSyncer(t, svc, "synth-running")
	s.SyncOnce(t.Context())
	sc := store.scans[digest]
	sc.CheckedAt = sc.CheckedAt.Add(-retryAfter)
	store.scans[digest] = sc
	s.SyncOnce(t.Context())
	if svc.art != 1 || svc.results != 2 {
		t.Errorf("ART read %d times, Results %d; want 1 and 2", svc.art, svc.results)
	}
}
