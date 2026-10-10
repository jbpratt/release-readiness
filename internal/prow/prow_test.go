package prow

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	periodicJob = "periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly"
	periodicRun = "2100388335968063488" // tested-images.json present
	pendingRun  = "2108233750193115136" // no finished.json yet
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func runFixture(t *testing.T, job Job, id, file string) []byte {
	t.Helper()
	path := filepath.Join("testdata", "gcs", job.Prefix+id, file)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testJobs(t *testing.T) []Job {
	t.Helper()
	jobs, err := ParseJobs(periodicJob + "=quay-3-18")
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestParseJobs(t *testing.T) {
	jobs := testJobs(t)
	want := Job{Name: periodicJob, Prefix: "logs/" + periodicJob + "/", Application: "quay-3-18"}
	if len(jobs) != 1 || jobs[0] != want {
		t.Errorf("ParseJobs = %+v, want %+v", jobs, want)
	}
	if _, err := ParseJobs("no-release"); err == nil {
		t.Error("ParseJobs without =application: want error")
	}
	if jobs, err := ParseJobs(""); err != nil || len(jobs) != 0 {
		t.Errorf("ParseJobs(\"\") = %v, %v, want none", jobs, err)
	}
}

func TestParseRun(t *testing.T) {
	job := testJobs(t)[0]

	r, target, err := parseRun(job, periodicRun, runFixture(t, job, periodicRun, "prowjob.json"), runFixture(t, job, periodicRun, "finished.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "failure" || r.CompletedAt == nil || r.StartedAt == nil || target != "aws-s3-nightly" ||
		r.Application != "quay-3-18" || r.ArtifactState != ArtifactMissing ||
		!strings.HasSuffix(r.ProwURL, "/"+periodicRun) {
		t.Errorf("finished run = %+v, target %q", r, target)
	}

	r, _, err = parseRun(job, pendingRun, runFixture(t, job, pendingRun, "prowjob.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "pending" || r.CompletedAt != nil {
		t.Errorf("pending run: state %q, completed %v", r.State, r.CompletedAt)
	}
}

func TestApplyArtifact(t *testing.T) {
	job := testJobs(t)[0]
	r := &Run{}
	r.applyArtifact(runFixture(t, job, periodicRun, "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json"))
	if r.ArtifactState != ArtifactPresent {
		t.Fatalf("artifact state = %q, want present", r.ArtifactState)
	}
	if !strings.HasSuffix(r.CatalogRef, "@sha256:16703125878630f03d1781454b1da8212db6127c822f95c2cfe04e805210bb0d") {
		t.Errorf("catalog ref = %q", r.CatalogRef)
	}
	// 11 pod images plus the catalog and the operator bundle.
	if len(r.Images) != 13 {
		t.Fatalf("images = %d, want 13", len(r.Images))
	}
	byRole := map[string]Image{}
	for _, img := range r.Images {
		if _, ok := byRole[img.Role]; !ok {
			byRole[img.Role] = img
		}
	}
	for role, want := range map[string]string{
		"catalog":              "sha256:16703125878630f03d1781454b1da8212db6127c822f95c2cfe04e805210bb0d",
		"quay-operator-bundle": "sha256:9dbd39a4c49b8879ae5290a22ff9c139168256d91950a956bc62097b6e978de0",
		"quay":                 "sha256:e0716d7b2f783366224c95db83a8f2eec7ea44ac1738e7a63190292ed0733877",
		// The join key is the requested digest, not the differing runtime imageID.
		"clair": "sha256:da60da674707e74aad96c1213a98025155870d53a19aa277fb45edeff38874d5",
	} {
		if got := byRole[role].Digest; got != want {
			t.Errorf("%s digest = %q, want %q", role, got, want)
		}
	}

	r = &Run{}
	r.applyArtifact([]byte(`{"schema_version": 1, "images": [{"role": "quay",`))
	if r.ArtifactState != ArtifactInvalid || len(r.Images) != 0 {
		t.Errorf("truncated artifact: state %q, %d images", r.ArtifactState, len(r.Images))
	}
}

func TestApplyArtifactCountsOnlyPodImages(t *testing.T) {
	const (
		quay    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		builder = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		catalog = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
		bundle  = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	)
	r := &Run{}
	r.applyArtifact([]byte(`{"schema_version": 1,
		"catalog": {"resolved_ref": "quay.io/x/art-fbc@` + catalog + `"},
		"operator": {"bundle_ref": "quay.io/x/bundle@` + bundle + `"},
		"images": [
			{"role": "quay", "requested_ref": "quay.io/x/art-images@` + quay + `", "source": "pod"},
			{"role": "builder", "requested_ref": "quay.io/x/art-images@` + builder + `", "source": "csv-related"}
		]}`))
	for _, tc := range []struct {
		app, component, digest string
		want                   bool
	}{
		{"quay-3-18", "quay-3-18-quay-quay", quay, true},
		{"fbc-quay-3-18", "fbc-quay-3-18-quay-operator", catalog, true},
		{"quay-3-18", "quay-3-18-quay-operator-bundle", bundle, true},
		{"quay-3-18", "quay-3-18-quay-builder", builder, false},
	} {
		key := ComponentKey(tc.app, tc.component, "quay.io/x/art-images@"+tc.digest)
		if got := r.Tested(map[string]bool{key: true}); got != tc.want {
			t.Errorf("Tested(%s) = %v, want %v", key, got, tc.want)
		}
	}
}

type memStore struct {
	runs  map[string]*Run
	syncs map[string]SyncState
}

func (m *memStore) ListFinishedProwBuildIDs(_ context.Context, job string) ([]string, error) {
	var ids []string
	for _, r := range m.runs {
		if r.JobName == job && r.CompletedAt != nil {
			ids = append(ids, r.BuildID)
		}
	}
	return ids, nil
}

func (m *memStore) UpsertProwRun(_ context.Context, r *Run) error {
	m.runs[r.BuildID] = r
	return nil
}

func (m *memStore) UpsertProwSync(_ context.Context, s SyncState) error {
	m.syncs[s.JobName] = s
	return nil
}

// fakeGCS serves recorded listing pages and objects from testdata, plus
// extra objects, and counts object requests.
func fakeGCS(t *testing.T, extra map[string][]byte) (*httptest.Server, map[string]int) {
	t.Helper()
	var mu sync.Mutex
	gets := map[string]int{}
	pages := map[string]string{
		"logs/" + periodicJob + "/|":       "list-periodic.json",
		"logs/" + periodicJob + "/|page-2": "list-periodic-2.json",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/storage/v1/b/"+bucket+"/o" {
			q := r.URL.Query()
			file, ok := pages[q.Get("prefix")+"|"+q.Get("pageToken")]
			if !ok {
				t.Errorf("unexpected listing %s", r.URL.RawQuery)
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(readFixture(t, file))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/"+bucket+"/")
		mu.Lock()
		gets[name]++
		mu.Unlock()
		if data, ok := extra[name]; ok {
			_, _ = w.Write(data)
			return
		}
		f, err := os.Open(filepath.Join("testdata", "gcs", name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		_, _ = io.Copy(w, f)
	}))
	t.Cleanup(srv.Close)
	return srv, gets
}

func TestSync(t *testing.T) {
	extra := map[string][]byte{}
	srv, gets := fakeGCS(t, extra)
	store := &memStore{runs: map[string]*Run{}, syncs: map[string]SyncState{}}
	s := NewSyncer(NewClient(srv.URL), store, testJobs(t), 15*time.Minute, slog.New(slog.DiscardHandler))
	reg := syncstatus.New()
	s.Status = reg.Track("prow", 15*time.Minute)

	s.SyncOnce(t.Context())

	if p := reg.Problems(time.Now()); len(p) != 0 {
		t.Errorf("problems after a clean pass = %+v", p)
	}
	want := map[string]struct{ state, artifact string }{
		periodicRun: {"failure", ArtifactPresent},
		pendingRun:  {"pending", ArtifactMissing},
	}
	if len(store.runs) != len(want) {
		t.Fatalf("runs = %d, want %d", len(store.runs), len(want))
	}
	for id, w := range want {
		r := store.runs[id]
		if r == nil || r.State != w.state || r.ArtifactState != w.artifact {
			t.Errorf("run %s = %+v, want %s/%s", id, r, w.state, w.artifact)
		}
	}
	if r := store.runs[periodicRun]; r.JobName != periodicJob || r.Application != "quay-3-18" {
		t.Errorf("periodic run = %+v", r)
	}
	if len(store.syncs) != 1 {
		t.Errorf("syncs = %v, want the job", store.syncs)
	}

	// The pending run finishes; finished runs are not fetched again.
	extra["logs/"+periodicJob+"/"+pendingRun+"/finished.json"] = []byte(`{"timestamp":1791480000,"passed":true,"result":"SUCCESS"}`)
	s.SyncOnce(t.Context())
	if r := store.runs[pendingRun]; r.State != "success" || r.CompletedAt == nil || !r.CompletedAt.Equal(time.Unix(1791480000, 0)) || r.ArtifactState != ArtifactMissing {
		t.Errorf("finished run = %+v", r)
	}
	for id, n := range map[string]int{periodicRun: 1, pendingRun: 2} {
		if got := gets["logs/"+periodicJob+"/"+id+"/prowjob.json"]; got != n {
			t.Errorf("prowjob.json fetches for %s = %d, want %d", id, got, n)
		}
	}
}

func TestSyncReportsFailedListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	jobs := append(testJobs(t), Job{Name: "other", Prefix: "logs/other/"})
	reg := syncstatus.New()
	s := NewSyncer(NewClient(srv.URL), &memStore{runs: map[string]*Run{}, syncs: map[string]SyncState{}}, jobs, 15*time.Minute, slog.New(slog.DiscardHandler))
	s.Status = reg.Track("prow", 15*time.Minute)

	s.SyncOnce(t.Context())

	p := reg.Problems(time.Now())
	if len(p) != 1 || p[0].Source != "prow" || !strings.Contains(p[0].Message, "503") || !strings.Contains(p[0].Message, "2 errors this pass") {
		t.Fatalf("problems = %+v, want one prow failure covering both jobs", p)
	}
}

func TestComponentKey(t *testing.T) {
	const d = "sha256:16703125878630f03d1781454b1da8212db6127c822f95c2cfe04e805210bb0d"
	for _, tc := range []struct{ app, component, image, want string }{
		{"quay-3-17", "quay-3-17-quay-quay", "quay.io/x/art-images@" + d, "quay@" + d},
		{"quay-3-17", "quay-3-17-quay-operator-bundle", "quay.io/x/art-images@" + d, "quay-operator-bundle@" + d},
		{"fbc-quay-3-17", "fbc-quay-3-17-quay-operator", "quay.io/x/art-fbc@" + d, "catalog@" + d},
		{"fbc-quay-3-17", "fbc-quay-3-17-quay-bridge-operator", "quay.io/x/art-fbc@" + d, ""},
		{"quay-3-17", "quay-3-17-base-rhel9", "quay.io/x/art-images@" + d, ""},
		{"quay-images-base", "quay-3-17-quay-quay", "quay.io/x/art-images@" + d, ""},
		{"quay-3-17", "quay-3-17-quay-quay", "quay.io/x/art-images:v3.17.6", ""},
	} {
		if got := ComponentKey(tc.app, tc.component, tc.image); got != tc.want {
			t.Errorf("ComponentKey(%s, %s) = %q, want %q", tc.app, tc.component, got, tc.want)
		}
	}
}
