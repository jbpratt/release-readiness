package prow

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	periodicJob = "periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly"
	periodicRun = "2100388335968063488" // tested-images.json present
	olmRun      = "2108360975915683840" // olm-csvs.yaml only
	noneRun     = "2106911479667101696" // neither
	pendingRun  = "2108233750193115136" // no finished.json yet
	olmCSVs     = "artifacts/aws-s3-nightly/quay-gather/artifacts/olm-csvs.yaml"
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
	// 11 pod images plus the operator bundle.
	if len(r.Images) != 12 {
		t.Fatalf("images = %d, want 12", len(r.Images))
	}
	byRole := map[string]Image{}
	for _, img := range r.Images {
		if _, ok := byRole[img.Role]; !ok {
			byRole[img.Role] = img
		}
	}
	for role, want := range map[string]string{
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

// olm-csvs.yaml names the Quay images of the quay-operator CSV the run
// installed, here quay-v3.18.1's release candidate's.
func TestApplyOLMCSVs(t *testing.T) {
	job := testJobs(t)[0]
	quay := runFixture(t, job, olmRun, olmCSVs)
	r := &Run{}
	r.applyOLMCSVs(quay)
	want := []Image{
		{"quay-operator", "sha256:261469689b60b3c285fa9c3492ad83daf30b93d6b5a860845dc803af1075dbfb"},
		{"quay", "sha256:4db6193f032e733fb80f8b83c5a3a0c4e43cda6fdbd7c9cbf150b1ee93bbab3c"},
		{"clair", "sha256:4953b4a9a79ea732e585b04deab4d2595ae21a71f33c5dd6b0c1d360160e16d8"},
		{"quay-builder", "sha256:ccae0d49a7795cf7e00a407441b4e404715b8fb21e575756d341467d59e12c04"},
		{"quay-builder-qemu", "sha256:fb921ff5608a90658f2133d5e0885a41c498542e7ca363267db7d2c1810282ec"},
	}
	if r.ArtifactState != ArtifactOLMCSVs || !slices.Equal(r.Images, want) {
		t.Errorf("quay CSV: state %q, images %v, want %v", r.ArtifactState, r.Images, want)
	}

	r = &Run{}
	r.applyOLMCSVs(readFixture(t, "olm-csvs-no-quay.yaml"))
	if r.ArtifactState != ArtifactOLMCSVs || len(r.Images) != 0 {
		t.Errorf("no quay CSV: state %q, images %v, want none", r.ArtifactState, r.Images)
	}

	// The operator's repository before about Oct 1.
	r = &Run{}
	r.applyOLMCSVs([]byte(strings.ReplaceAll(string(quay), "/quay-rhel9-operator@", "/quay-operator-rhel9@")))
	if !slices.Equal(r.Images, want) {
		t.Errorf("quay-operator-rhel9: images %v, want %v", r.Images, want)
	}

	// The CSV names a second builder: which one ran is unknown.
	two := strings.Replace(string(quay), want[3].Digest, "sha256:"+strings.Repeat("0", 64), 1)
	r = &Run{}
	r.applyOLMCSVs([]byte(two))
	if wantFour := slices.Delete(slices.Clone(want), 3, 4); !slices.Equal(r.Images, wantFour) {
		t.Errorf("two builders: images %v, want %v", r.Images, wantFour)
	}
}

type memStore struct {
	runs map[string]*Run
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

// fakeGCS serves recorded listing pages and objects from testdata, plus
// extra objects (a nil one answers 503), and counts object requests.
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
			if data == nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
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
	store := &memStore{runs: map[string]*Run{}}
	s := NewSyncer(NewClient(srv.URL), store, testJobs(t), 15*time.Minute, slog.New(slog.DiscardHandler))
	reg := syncstatus.New()
	s.Status = reg.Track("prow", 15*time.Minute)

	s.SyncOnce(t.Context())

	if p := reg.Problems(time.Now()); len(p) != 0 {
		t.Errorf("problems after a clean pass = %+v", p)
	}
	want := map[string]struct {
		state, artifact string
		images          int
	}{
		periodicRun: {"failure", ArtifactPresent, 12},
		olmRun:      {"success", ArtifactOLMCSVs, 5},
		noneRun:     {"failure", ArtifactNone, 0},
		pendingRun:  {"pending", ArtifactMissing, 0},
	}
	if len(store.runs) != len(want) {
		t.Fatalf("runs = %d, want %d", len(store.runs), len(want))
	}
	for id, w := range want {
		r := store.runs[id]
		if r == nil || r.State != w.state || r.ArtifactState != w.artifact || len(r.Images) != w.images {
			t.Errorf("run %s = %+v, want %s/%s with %d images", id, r, w.state, w.artifact, w.images)
		}
	}
	if r := store.runs[periodicRun]; r.JobName != periodicJob || r.Application != "quay-3-18" {
		t.Errorf("periodic run = %+v", r)
	}
	// olm-csvs.yaml is read only without tested-images.json.
	for id, n := range map[string]int{periodicRun: 0, olmRun: 1, noneRun: 1} {
		if got := gets["logs/"+periodicJob+"/"+id+"/"+olmCSVs]; got != n {
			t.Errorf("olm-csvs.yaml fetches for %s = %d, want %d", id, got, n)
		}
	}

	// The pending run finishes, but reading its olm-csvs.yaml fails: the pass
	// reports it and the run stays missing, so the next pass reads it again.
	pending := "logs/" + periodicJob + "/" + pendingRun + "/"
	extra[pending+"finished.json"] = []byte(`{"timestamp":1791480000,"passed":true,"result":"SUCCESS"}`)
	extra[pending+olmCSVs] = nil
	s.SyncOnce(t.Context())
	if p := reg.Problems(time.Now()); len(p) != 1 || !strings.Contains(p[0].Message, olmCSVs) {
		t.Errorf("problems after a failed olm-csvs.yaml read = %+v", p)
	}
	if r := store.runs[pendingRun]; r.ArtifactState != ArtifactMissing {
		t.Errorf("run after a failed olm-csvs.yaml read = %+v, want it missing", r)
	}

	// Finished runs are not fetched again.
	delete(extra, pending+olmCSVs)
	s.SyncOnce(t.Context())
	if r := store.runs[pendingRun]; r.State != "success" || r.CompletedAt == nil || !r.CompletedAt.Equal(time.Unix(1791480000, 0)) || r.ArtifactState != ArtifactNone {
		t.Errorf("finished run = %+v", r)
	}
	for id, n := range map[string]int{periodicRun: 1, olmRun: 1, noneRun: 1, pendingRun: 3} {
		if got := gets["logs/"+periodicJob+"/"+id+"/prowjob.json"]; got != n {
			t.Errorf("prowjob.json fetches for %s = %d, want %d", id, got, n)
		}
	}
}

// The Quay install step's finished.json is read only for a finished run that
// failed after quay-gather saw its quay-operator Subscription, and tells
// whether the install failed.
func TestSyncInstallFailed(t *testing.T) {
	job := testJobs(t)[0]
	const (
		// Synthetic: run 2108723415929065472, which passed, made to fail its
		// Quay install before OLM installed a CSV.
		installRun = "2108723415929065472"
		// Died before the Quay install: no Subscription.
		preInstallRun = "2109085596919009280"
		deploy        = "artifacts/aws-s3-nightly/quay-deploy-aws-s3/finished.json"
	)
	dir := func(id string) string { return "logs/" + periodicJob + "/" + id + "/" }
	for _, tc := range []struct {
		desc  string
		id    string
		extra map[string][]byte
		want  bool
		reads int    // of step outcomes
		warn  string // in the log, which is otherwise empty
	}{
		{desc: "install failed before a CSV", id: installRun, want: true, reads: 1},
		{desc: "died before the install", id: preInstallRun},
		{desc: "install failed after the CSV", id: periodicRun, want: true, reads: 1, extra: map[string][]byte{
			dir(periodicRun) + deploy: []byte(`{"timestamp":1789612696,"passed":false,"result":"FAILURE"}`),
		}},
		{desc: "installed, then failed", id: periodicRun, reads: 1},
		{desc: "passed", id: installRun, extra: map[string][]byte{
			dir(installRun) + "finished.json": []byte(`{"timestamp":1791602726,"passed":true,"result":"SUCCESS"}`),
		}},
		{desc: "no tested-images.json", id: noneRun},
		{desc: "invalid tested-images.json", id: periodicRun, extra: map[string][]byte{
			dir(periodicRun) + "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json": []byte(`{"schema_version": 1, "images": [{"role": "quay",`),
		}},
		{desc: "unmapped target", id: periodicRun, warn: "job=" + periodicJob + " build_id=" + periodicRun + " target=other-nightly", extra: map[string][]byte{
			dir(periodicRun) + "prowjob.json": []byte(`{"spec": {"pod_spec": {"containers": [{"args": ["--target=other-nightly"]}]}}, "status": {"state": "failure"}}`),
			dir(periodicRun) + "artifacts/other-nightly/quay-gather/artifacts/tested-images.json": runFixture(t, job, periodicRun, "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json"),
		}},
	} {
		srv, gets := fakeGCS(t, tc.extra)
		var log strings.Builder
		s := NewSyncer(NewClient(srv.URL), nil, []Job{job}, 15*time.Minute, slog.New(slog.NewTextHandler(&log, nil)))
		r, err := s.fetchRun(t.Context(), job, tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.desc, err)
		}
		reads := 0
		for name, n := range gets {
			if strings.Contains(name, "/artifacts/") && !strings.Contains(name, "/quay-gather/") {
				reads += n
			}
		}
		if r.InstallFailed != tc.want || reads != tc.reads {
			t.Errorf("%s: install failed %v after %d step reads, want %v after %d", tc.desc, r.InstallFailed, reads, tc.want, tc.reads)
		}
		if got := log.String(); (got == "") != (tc.warn == "") || !strings.Contains(got, tc.warn) {
			t.Errorf("%s: log %q, want %q", tc.desc, got, tc.warn)
		}
	}

	// An unreadable step outcome fails the read, so the next pass reads the
	// run again.
	srv, _ := fakeGCS(t, map[string][]byte{dir(periodicRun) + deploy: nil})
	s := NewSyncer(NewClient(srv.URL), nil, []Job{job}, 15*time.Minute, slog.New(slog.DiscardHandler))
	if _, err := s.fetchRun(t.Context(), job, periodicRun); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("unreadable step outcome: error %v, want a 503", err)
	}
}

// An invalid tested-images.json is the run's record: olm-csvs.yaml is not
// read in its place.
func TestSyncInvalidTestedImages(t *testing.T) {
	run := "logs/" + periodicJob + "/" + olmRun + "/"
	srv, gets := fakeGCS(t, map[string][]byte{
		run + "artifacts/aws-s3-nightly/quay-gather/artifacts/tested-images.json": []byte(`{"schema_version": 1, "images": [{"role": "quay",`),
	})
	store := &memStore{runs: map[string]*Run{}}
	NewSyncer(NewClient(srv.URL), store, testJobs(t), 15*time.Minute, slog.New(slog.DiscardHandler)).SyncOnce(t.Context())
	if r, n := store.runs[olmRun], gets[run+olmCSVs]; r == nil || r.ArtifactState != ArtifactInvalid || n != 0 {
		t.Errorf("run = %+v after %d olm-csvs.yaml fetches, want invalid after none", r, n)
	}
}

func TestSyncReportsFailedListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	jobs := append(testJobs(t), Job{Name: "other", Prefix: "logs/other/"})
	reg := syncstatus.New()
	s := NewSyncer(NewClient(srv.URL), &memStore{runs: map[string]*Run{}}, jobs, 15*time.Minute, slog.New(slog.DiscardHandler))
	s.Status = reg.Track("prow", 15*time.Minute)

	s.SyncOnce(t.Context())

	p := reg.Problems(time.Now())
	if len(p) != 1 || p[0].Source != "prow" || !strings.Contains(p[0].Message, "503") || !strings.Contains(p[0].Message, "2 errors this pass") {
		t.Fatalf("problems = %+v, want one prow failure covering both jobs", p)
	}
}
