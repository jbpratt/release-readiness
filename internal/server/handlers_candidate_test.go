package server

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/scan"
)

// quay318 is a quay-3-18 build's components less its base image, by name.
var quay318 = []string{
	"quay-3-18-container-security-operator",
	"quay-3-18-container-security-operator-bundle",
	"quay-3-18-quay-bridge-operator",
	"quay-3-18-quay-bridge-operator-bundle",
	"quay-3-18-quay-builder",
	"quay-3-18-quay-builder-qemu",
	"quay-3-18-quay-clair",
	"quay-3-18-quay-operator",
	"quay-3-18-quay-operator-bundle",
	"quay-3-18-quay-quay",
}

// testDigest is the digest of the image labelled <build>/<component>.
func testDigest(label string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(label)))
}

func testImage(label string) string {
	return "quay.io/redhat-user-workloads/ocp-art-tenant/art-images@" + testDigest(label)
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// seedQuay318 seeds quay-3-18 for quay-v3.18.1 and quay-v3.18.2, shaped on
// 10 Oct 2026: 3.18.1's STAGE build "stage" and its failed prod Release, the
// build "tested" the periodics ran, and the stream's newest build "new", whose
// images are staged only for both bundles and the builder.
func seedQuay318(t *testing.T, srv *Server) {
	t.Helper()
	ctx := t.Context()
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	for _, v := range []string{"quay-v3.18.1", "quay-v3.18.2"} {
		if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: v, KonfluxApplication: "quay-3-18"}); err != nil {
			t.Fatal(err)
		}
	}
	build := func(label string, comps ...string) map[string]string {
		images := map[string]string{}
		for _, c := range comps {
			images[c] = testImage(label + "/" + c)
		}
		return images
	}
	snapshot := func(name, created string, images map[string]string) {
		t.Helper()
		id, err := srv.db.CreateSnapshot(ctx, "quay-3-18", name, mustTime(t, created))
		if err != nil {
			t.Fatal(err)
		}
		for c, img := range images {
			if err := srv.db.CreateSnapshotComponent(ctx, id, c, img); err != nil {
				t.Fatal(err)
			}
		}
	}
	assembly := func(name, env, created string) {
		t.Helper()
		if err := srv.db.UpsertStagedSnapshot(ctx, name, "3.18.1", "image", env, mustTime(t, created)); err != nil {
			t.Fatal(err)
		}
	}
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")

	staged := build("stage", quay318...)
	snapshot("quay-stage-3-18-1-image-20261007200243", "2026-10-07T20:02:43Z", staged)
	assembly("quay-stage-3-18-1-image-20261007200243", "stage", "2026-10-07T20:02:43Z")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261007200243", "quay-stage-3-18-1-image-20261007200243",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-07T20:02:43Z", "2026-10-07T20:14:57Z")
	snapshot("quay-3-18-20261009-042610-000", "2026-10-09T04:29:06Z", build("tested", all...))
	ri := build("tested", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay")
	ri["quay-3-18-quay-builder"] = testImage("new/quay-3-18-quay-builder")
	snapshot("fbc-ri-stage-quay-3-18-quay-operator-jxfbj", "2026-10-09T04:29:19Z", ri)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-r8fcb", "fbc-ri-stage-quay-3-18-quay-operator-jxfbj",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-09T04:29:20Z", "2026-10-09T04:41:33Z")
	snapshot("quay-prod-3-18-1-image-20261009133455", "2026-10-09T13:34:55Z", staged)
	assembly("quay-prod-3-18-1-image-20261009133455", "prod", "2026-10-09T13:34:55Z")
	seedKonfluxRelease(t, srv, "quay-prod-3-18-1-image-20261009133455", "quay-prod-3-18-1-image-20261009133455",
		"quay-advisory-prod-3-18", "Failed", "2026-10-09T13:34:55Z", "2026-10-09T13:39:03Z")

	newest := build("new", all...)
	for _, c := range []string{"quay-3-18-container-security-operator-bundle", "quay-3-18-quay-bridge-operator-bundle"} {
		newest[c] = staged[c]
	}
	snapshot("quay-3-18-20261010-013713-000", "2026-10-10T01:48:34Z", newest)
	// Started later, but created first.
	snapshot("quay-3-18-20261010-013900-000", "2026-10-10T01:40:00Z", build("other", all...))
	// Newer, but not stream builds. 3.18.1's newer assembly failed stage.
	snapshot("fbc-ri-stage-quay-3-18-quay-operator-d552l", "2026-10-10T01:49:22Z",
		build("new", "quay-3-18-quay-builder", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay"))
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-ssrlx", "fbc-ri-stage-quay-3-18-quay-operator-d552l",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T01:49:22Z", "2026-10-10T01:52:21Z")
	snapshot("quay-stage-3-18-1-image-20261010020000", "2026-10-10T02:00:00Z", staged)
	assembly("quay-stage-3-18-1-image-20261010020000", "stage", "2026-10-10T02:00:00Z")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261010020000", "quay-stage-3-18-1-image-20261010020000",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T02:00:00Z", "2026-10-10T02:05:00Z")
	snapshot("quay-prod-3-18-1-image-20261010030000", "2026-10-10T03:00:00Z", staged)
	assembly("quay-prod-3-18-1-image-20261010030000", "prod", "2026-10-10T03:00:00Z")

	for _, b := range []artbuild.Build{
		{Digest: testDigest("new/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: "quay-quay-container-3.18.1-202610092112.p2.gec1e520.assembly.stream.el9", RecordID: "rec-quay"},
		{Digest: testDigest("new/quay-3-18-quay-clair"), State: artbuild.StateUnresolved},
		{Digest: testDigest("tested/quay-3-18-quay-operator-bundle"), State: artbuild.StateResolved, NVR: "quay-operator-metadata-container-3.18.1.202610090339.p2.g35cf767.assembly.stream.el9-1", RecordID: "rec-bundle"},
	} {
		b.CheckedAt = time.Now()
		if err := srv.db.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
}

// seedKonfluxRelease stores a quay-3-18 Release that ended with reason; a
// Failed one stopped at verify-conforma.
func seedKonfluxRelease(t *testing.T, srv *Server, name, snapshot, plan, reason, created, completed string) {
	t.Helper()
	r := &model.KonfluxRelease{
		Name: name, Application: "quay-3-18", Snapshot: snapshot, ReleasePlan: plan,
		ReleasedStatus: "False", ReleasedReason: reason, CreatedAt: mustTime(t, created),
	}
	end := mustTime(t, completed)
	r.CompletionTime = &end
	if reason == "Succeeded" {
		r.ReleasedStatus = "True"
	}
	if reason == "Failed" {
		r.FailedTask, r.FailedStep = "verify-conforma", "assert"
	}
	if err := srv.db.UpsertKonfluxRelease(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestGetCandidate(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	names := func(cs []model.CandidateComponent) []string {
		out := []string{}
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}

	// 3.18.1 has a STAGE build; its prod Release is the newest Release of a
	// prod Snapshot, not the newest prod Snapshot.
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	c := got.Candidate
	if got.Shipped || got.Reason != "" || c == nil || c.Snapshot != "quay-stage-3-18-1-image-20261007200243" || c.Source != "staged" ||
		!c.CreatedAt.Equal(mustTime(t, "2026-10-07T20:02:43Z")) || c.StagedAt == nil || !c.StagedAt.Equal(mustTime(t, "2026-10-07T20:14:57Z")) {
		t.Fatalf("3.18.1 candidate = %+v, reason %q", c, got.Reason)
	}
	if !slices.Equal(names(c.Components), quay318) || c.Components[0].Image != testImage("stage/"+quay318[0]) {
		t.Errorf("3.18.1 components = %+v", c.Components)
	}
	if p := got.Prod; p == nil || p.Name != "quay-prod-3-18-1-image-20261009133455" || p.ReleasedReason != "Failed" || p.FailedTask != "verify-conforma" {
		t.Errorf("3.18.1 prod = %+v", p)
	}

	// 3.18.2 has none staged: the stream's newest build, though newer
	// Snapshots ART assembled or re-released exist.
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	c = got.Candidate
	if c == nil || c.Snapshot != "quay-3-18-20261010-013713-000" || c.Source != "newest" || c.StagedAt != nil || got.Prod != nil {
		t.Fatalf("3.18.2 candidate = %+v, prod %+v", c, got.Prod)
	}
	if !slices.Equal(names(c.Components), quay318) {
		t.Fatalf("3.18.2 components = %v, want %v without the base image", names(c.Components), quay318)
	}
	wantQuay := model.CandidateComponent{
		Name:     "quay-3-18-quay-quay",
		Image:    testImage("new/quay-3-18-quay-quay"),
		NVR:      "quay-quay-container-3.18.1-202610092112.p2.gec1e520.assembly.stream.el9",
		BuildURL: "https://art.example/build?nvr=quay-quay-container-3.18.1-202610092112.p2.gec1e520.assembly.stream.el9&record_id=rec-quay",
	}
	wantClair := model.CandidateComponent{Name: "quay-3-18-quay-clair", Image: testImage("new/quay-3-18-quay-clair")}
	if c.Components[9] != wantQuay || c.Components[6] != wantClair {
		t.Errorf("3.18.2 quay, clair = %+v, %+v; want %+v, %+v", c.Components[9], c.Components[6], wantQuay, wantClair)
	}

	// No STAGE Release of quay-3-14 at all: the flag cannot tell.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14"}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"),
		"quay-3-14-base-rhel8", "quay-3-14-quay-builder", "quay-3-14-quay-builder-qemu", "quay-3-14-quay-clair")
	var raw map[string]any
	getJSON(t, srv, "/api/v1/releases/quay-v3.14.10/candidate", http.StatusOK, &raw)
	wantStage := map[string]any{
		"state": "unknown", "snapshot": "quay-3-14-20261009-185801-000", "created_at": "2026-10-09T19:27:56Z",
		"total": float64(3), "not_staged": []any{}, "release": nil,
	}
	if !reflect.DeepEqual(raw["stage"], wantStage) || !reflect.DeepEqual(raw["ci"], map[string]any{"jobs": []any{}, "last_tested": nil}) {
		t.Errorf("3.14.10 stage, ci = %v, %v; want %v and no CI", raw["stage"], raw["ci"], wantStage)
	}
}

// The stage flag is on the stream's newest build whichever build is the
// candidate, and names the failed STAGE Release holding its unstaged images.
func TestGetCandidateStage(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)

	var got, older model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &older)
	f := got.Stage
	wantNotStaged := []string{
		"quay-3-18-container-security-operator",
		"quay-3-18-quay-bridge-operator",
		"quay-3-18-quay-builder-qemu",
		"quay-3-18-quay-clair",
		"quay-3-18-quay-operator",
		"quay-3-18-quay-operator-bundle",
		"quay-3-18-quay-quay",
	}
	if f == nil || f.State != "not_staged" || f.Snapshot != "quay-3-18-20261010-013713-000" || f.Total != 10 ||
		!slices.Equal(f.NotStaged, wantNotStaged) || f.Release == nil || f.Release.Name != "fbc-ri-stage-quay-3-18-quay-operator-ssrlx" {
		t.Fatalf("3.18.2 stage = %+v", f)
	}
	if !reflect.DeepEqual(older.Stage, f) {
		t.Errorf("3.18.1 stage = %+v, want 3.18.2's %+v", older.Stage, f)
	}

	// ssrlx is retried, and the CSO and QBO operators reach stage too.
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-ssrlx", "fbc-ri-stage-quay-3-18-quay-operator-d552l",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T01:49:22Z", "2026-10-10T02:30:00Z")
	id, err := srv.db.CreateSnapshot(t.Context(), "quay-3-18", "fbc-ri-stage-quay-3-18-operators", mustTime(t, "2026-10-10T02:40:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"quay-3-18-container-security-operator", "quay-3-18-quay-bridge-operator"} {
		if err := srv.db.CreateSnapshotComponent(t.Context(), id, c, testImage("new/"+c)); err != nil {
			t.Fatal(err)
		}
	}
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-operators", "fbc-ri-stage-quay-3-18-operators",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T02:40:00Z", "2026-10-10T02:50:00Z")
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if f := got.Stage; f == nil || f.State != "staged" || f.Total != 10 || len(f.NotStaged) != 0 || f.Release != nil {
		t.Errorf("all staged: stage = %+v", f)
	}
}

// A run tested the candidate only when it recorded the candidate's quay,
// clair, operator and bundle digests; else the newest build any run fully
// recorded is shown as last tested.
func TestGetCandidateCI(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	const prefix = "periodic-ci-quay-quay-redhat-3.18-"
	images := func(quay, clair, operator, bundle string) []prow.Image {
		return []prow.Image{
			{Role: "quay", Digest: testDigest(quay + "/quay-3-18-quay-quay")},
			{Role: "clair", Digest: testDigest(clair + "/quay-3-18-quay-clair")},
			{Role: "quay-operator", Digest: testDigest(operator + "/quay-3-18-quay-operator")},
			{Role: "quay-operator-bundle", Digest: testDigest(bundle + "/quay-3-18-quay-operator-bundle")},
		}
	}
	for _, r := range []struct {
		job, id, state, started string
		images                  []prow.Image
	}{
		{"aws-ocp422-e2e-install-aws-s3-nightly", "1", "failure", "2026-10-08T00:56:53Z", images("stage", "stage", "stage", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "2", "success", "2026-10-08T12:56:53Z", images("stage", "stage", "stage", "stage")},
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "3", "success", "2026-10-08T11:49:53Z", images("stage", "stage", "stage", "stage")},
		// The staged bundle beside another clair is another build.
		{"azure-ocp422-e2e-install-azure-blob-nightly", "4", "success", "2026-10-08T13:27:53Z", images("stage", "tested", "stage", "stage")},
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "5", "success", "2026-10-09T23:49:53Z", images("tested", "tested", "tested", "tested")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "6", "success", "2026-10-10T00:56:53Z", images("tested", "tested", "tested", "tested")},
		{"azure-ocp422-e2e-install-azure-blob-nightly", "7", "failure", "2026-10-10T01:27:53Z", images("tested", "tested", "tested", "tested")},
		{"aws-ocp422-e2e-install-aws-odf-nightly", "8", "success", "2026-10-10T03:11:53Z", images("tested", "tested", "tested", "tested")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "9", "success", "2026-10-10T04:00:00Z", nil},
	} {
		started := mustTime(t, r.started)
		run := &prow.Run{
			JobName: prefix + r.job, BuildID: r.id, Application: "quay-3-18", State: r.state, StartedAt: &started,
			ProwURL: "https://prow.example/" + r.id, ArtifactState: prow.ArtifactPresent, FetchedAt: started, Images: r.images,
		}
		if r.images == nil {
			run.ArtifactState = prow.ArtifactMissing
		}
		if err := srv.db.UpsertProwRun(t.Context(), run); err != nil {
			t.Fatal(err)
		}
	}
	job := func(name, state, id, started string, runs int) model.CIJob {
		at := mustTime(t, started)
		return model.CIJob{JobName: prefix + name, State: state, ProwURL: "https://prow.example/" + id, StartedAt: &at, Runs: runs}
	}

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want := model.CandidateCI{Jobs: []model.CIJob{
		job("aws-ocp422-e2e-install-aws-s3-nightly", "success", "2", "2026-10-08T12:56:53Z", 2),
		job("gcp-ocp422-e2e-install-gcp-gcs-nightly", "success", "3", "2026-10-08T11:49:53Z", 1),
	}}
	if !reflect.DeepEqual(got.CI, want) {
		t.Errorf("3.18.1 ci = %+v, want %+v", got.CI, want)
	}

	// No run tested 3.18.2's newest build.
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	testedAt := mustTime(t, "2026-10-10T03:11:53Z")
	want = model.CandidateCI{Jobs: []model.CIJob{}, LastTested: &model.LastTested{
		BundleNVR: "quay-operator-metadata-container-3.18.1.202610090339.p2.g35cf767.assembly.stream.el9-1",
		TestedAt:  &testedAt,
		Jobs: []model.CIJob{
			job("aws-ocp422-e2e-install-aws-odf-nightly", "success", "8", "2026-10-10T03:11:53Z", 1),
			job("aws-ocp422-e2e-install-aws-s3-nightly", "success", "6", "2026-10-10T00:56:53Z", 1),
			job("azure-ocp422-e2e-install-azure-blob-nightly", "failure", "7", "2026-10-10T01:27:53Z", 1),
			job("gcp-ocp422-e2e-install-gcp-gcs-nightly", "success", "5", "2026-10-09T23:49:53Z", 1),
		},
	}}
	if !reflect.DeepEqual(got.CI, want) {
		t.Errorf("3.18.2 ci = %+v, want %+v", got.CI, want)
	}
}

// A shipped version has nothing to decide; a version ART cannot build says
// why it has no candidate.
func TestGetCandidateNone(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":1,"data":[{"repositories":[{"published":true,"tags":[{"name":"v3.18.3"}]}]}]}`))
	}))
	defer cat.Close()
	srv.shipped = catalog.NewShipped(catalog.NewClient(cat.URL, cat.Client()), slog.Default())
	srv.shipped.Refresh(t.Context())
	for _, v := range []model.ReleaseVersion{
		{Name: "quay-v3.18.0", KonfluxApplication: "quay-3-18", Released: true},
		{Name: "quay-v3.18.3", KonfluxApplication: "quay-3-18"},
		{Name: "omr-v2.0.13", KonfluxApplication: "omr-2-0"},
		{Name: "quay-v3.20.0", KonfluxApplication: "quay-3-20"},
		{Name: "3.16.3"},
	} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &v); err != nil {
			t.Fatal(err)
		}
	}
	none := func(shipped bool, reason string) map[string]any {
		return map[string]any{
			"shipped": shipped, "candidate": nil, "reason": reason, "stage": nil, "prod": nil,
			"ci": map[string]any{"jobs": []any{}, "last_tested": nil},
		}
	}
	for version, want := range map[string]map[string]any{
		"quay-v3.18.0": none(true, ""), // released in JIRA
		"quay-v3.18.3": none(true, ""), // published in the catalog
		"omr-v2.0.13":  none(false, "not a concrete quay-vX.Y.Z version"),
		"3.16.3":       none(false, "no Konflux application"),
		"quay-v3.20.0": none(false, "no build of quay-3-20"),
	} {
		var got map[string]any
		getJSON(t, srv, "/api/v1/releases/"+version+"/candidate", http.StatusOK, &got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", version, got, want)
		}
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/candidate", http.StatusNotFound, nil)
}

// A component's scan is its image's stored scan, null without one.
func TestGetCandidateScan(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	const page = "https://konflux-ui.example/ns/art-quay-tenant/applications/quay-3-18/pipelineruns/"
	for _, sc := range []scan.Scan{
		{
			Digest: testDigest("new/quay-3-18-quay-operator"), State: scan.StateScanned,
			PipelineRun: "quay-3-18-quay-operator-7mbwk", DetailURL: page + "quay-3-18-quay-operator-7mbwk",
			Counts:  &model.ScanCounts{Basis: scan.BasisArchFindings, NoFix: model.SeverityCounts{High: 24, Medium: 264, Low: 232}},
			Reports: map[string]string{"sha256:arch": "sha256:report"},
		},
		{
			Digest: testDigest("new/quay-3-18-quay-quay"), State: scan.StateScanFailed,
			PipelineRun: "quay-3-18-quay-quay-zwvxj", DetailURL: page + "quay-3-18-quay-quay-zwvxj",
		},
	} {
		sc.CheckedAt = time.Now()
		if err := srv.db.UpsertImageScan(t.Context(), sc); err != nil {
			t.Fatal(err)
		}
	}

	var got struct {
		Candidate struct {
			Components []map[string]any `json:"components"`
		} `json:"candidate"`
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	severities := func(high, medium, low float64) map[string]any {
		return map[string]any{"critical": 0.0, "high": high, "medium": medium, "low": low, "unknown": 0.0}
	}
	for name, want := range map[string]any{
		"quay-3-18-quay-operator": map[string]any{
			"state": "scanned", "url": page + "quay-3-18-quay-operator-7mbwk",
			"counts": map[string]any{"basis": "scanner_arch_findings", "fixable": severities(0, 0, 0), "no_fix": severities(24, 264, 232)},
		},
		"quay-3-18-quay-quay":  map[string]any{"state": "scan_failed", "url": page + "quay-3-18-quay-quay-zwvxj", "counts": nil},
		"quay-3-18-quay-clair": nil,
	} {
		i := slices.IndexFunc(got.Candidate.Components, func(c map[string]any) bool { return c["name"] == name })
		if i < 0 {
			t.Fatalf("%s: no such component", name)
		}
		if sc, ok := got.Candidate.Components[i]["scan"]; !ok || !reflect.DeepEqual(sc, want) {
			t.Errorf("%s scan = %v, want %v", name, sc, want)
		}
	}
}

// The scan sync reads the candidate images of every version not archived.
func TestCandidateDigests(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14", Archived: true}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"), "quay-3-14-quay-clair")

	got, err := srv.CandidateDigests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 3.18.1's STAGE build, and 3.18.2's newest build less the two bundles
	// it shares with it.
	shared := []string{"quay-3-18-container-security-operator-bundle", "quay-3-18-quay-bridge-operator-bundle"}
	var want []string
	for _, c := range quay318 {
		want = append(want, testDigest("stage/"+c))
		if !slices.Contains(shared, c) {
			want = append(want, testDigest("new/"+c))
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("digests = %v, want %v", got, want)
	}
}
