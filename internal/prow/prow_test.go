package prow

import (
	"context"
	"encoding/json"
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
)

const (
	rehearsalPrefix = "pr-logs/pull/openshift_release/86270/rehearse-86270-periodic-ci-quay-quay-redhat-3.17-aws-ocp422-e2e-install-aws-s3-nightly"
	periodicJob     = "periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly"
	failedRun       = "2108168511183392768" // tested-images.json present
	invalidRun      = "2105463438045089792" // tested-images.json truncated
	pendingRun      = "2108233750193115136" // no finished.json yet
	periodicRun     = "2100388335968063488" // no tested-images.json
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
	periodic, err := ParseJobs(periodicJob+"=quay-v3.18.0", KindPeriodic)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := ParseJobs(rehearsalPrefix+"/=quay-v3.17.0", KindRehearsal)
	if err != nil {
		t.Fatal(err)
	}
	return append(periodic, rehearsal...)
}

func TestParseJobs(t *testing.T) {
	jobs := testJobs(t)
	want := []Job{
		{Name: periodicJob, Prefix: "logs/" + periodicJob + "/", ReleaseVersion: "quay-v3.18.0", Kind: KindPeriodic},
		{Name: "rehearse-86270-periodic-ci-quay-quay-redhat-3.17-aws-ocp422-e2e-install-aws-s3-nightly", Prefix: rehearsalPrefix + "/", ReleaseVersion: "quay-v3.17.0", Kind: KindRehearsal},
	}
	if len(jobs) != 2 || jobs[0] != want[0] || jobs[1] != want[1] {
		t.Errorf("ParseJobs = %+v, want %+v", jobs, want)
	}
	if _, err := ParseJobs("no-release", KindPeriodic); err == nil {
		t.Error("ParseJobs without =release_version: want error")
	}
	if jobs, err := ParseJobs("", KindPeriodic); err != nil || len(jobs) != 0 {
		t.Errorf("ParseJobs(\"\") = %v, %v, want none", jobs, err)
	}
}

func TestParseRun(t *testing.T) {
	job := testJobs(t)[1]

	r, target, err := parseRun(job, failedRun, runFixture(t, job, failedRun, "prowjob.json"), runFixture(t, job, failedRun, "finished.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "failure" || r.CompletedAt == nil || r.StartedAt == nil || target != "aws-s3-nightly" ||
		r.Kind != KindRehearsal || r.ReleaseVersion != "quay-v3.17.0" || r.ArtifactState != ArtifactMissing ||
		!strings.HasSuffix(r.ProwURL, "/"+failedRun) {
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
	job := testJobs(t)[1]
	r := &Run{Images: []Image{}}
	r.applyArtifact(runFixture(t, job, failedRun, "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json"))
	if r.ArtifactState != ArtifactPresent {
		t.Fatalf("artifact state = %q, want present", r.ArtifactState)
	}
	if !strings.HasSuffix(r.CatalogRef, "@sha256:16703125878630f03d1781454b1da8212db6127c822f95c2cfe04e805210bb0d") {
		t.Errorf("catalog ref = %q", r.CatalogRef)
	}
	// 19 images plus the catalog and the operator bundle.
	if len(r.Images) != 21 {
		t.Fatalf("images = %d, want 21", len(r.Images))
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
	if c := byRole["clair"]; c.Source != "pod" || !strings.HasSuffix(c.ImageID, "@sha256:a227858e891a176be8b46cd90d2f4392e5fdccc46dbfc30567cd8c10049e36d2") {
		t.Errorf("clair image = %+v", c)
	}

	r = &Run{Images: []Image{}}
	r.applyArtifact(runFixture(t, job, invalidRun, "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json"))
	if r.ArtifactState != ArtifactInvalid || len(r.Images) != 0 {
		t.Errorf("truncated artifact: state %q, %d images", r.ArtifactState, len(r.Images))
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
	page1 := readFixture(t, "list-rehearsal-1.json")
	var p1 struct {
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(page1, &p1); err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{
		"logs/" + periodicJob + "/|":              "list-periodic.json",
		rehearsalPrefix + "/|":                    "list-rehearsal-1.json",
		rehearsalPrefix + "/|" + p1.NextPageToken: "list-rehearsal-2.json",
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

	s.SyncOnce(t.Context())

	want := map[string]struct{ state, artifact string }{
		periodicRun: {"failure", ArtifactMissing},
		failedRun:   {"failure", ArtifactPresent},
		invalidRun:  {"failure", ArtifactInvalid},
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
	if r := store.runs[periodicRun]; r.Kind != KindPeriodic || r.JobName != periodicJob || r.ReleaseVersion != "quay-v3.18.0" {
		t.Errorf("periodic run = %+v", r)
	}
	if len(store.syncs) != 2 {
		t.Errorf("syncs = %v, want both jobs", store.syncs)
	}

	// The pending run finishes; finished runs are not fetched again.
	extra[rehearsalPrefix+"/"+pendingRun+"/finished.json"] = []byte(`{"timestamp":1791480000,"passed":true,"result":"SUCCESS"}`)
	s.SyncOnce(t.Context())
	if r := store.runs[pendingRun]; r.State != "success" || r.CompletedAt == nil || !r.CompletedAt.Equal(time.Unix(1791480000, 0)) {
		t.Errorf("finished run = %+v", r)
	}
	for id, n := range map[string]int{failedRun: 1, invalidRun: 1, pendingRun: 2} {
		if got := gets[rehearsalPrefix+"/"+id+"/prowjob.json"]; got != n {
			t.Errorf("prowjob.json fetches for %s = %d, want %d", id, got, n)
		}
	}
}
