package server

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"maps"
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

// testBuild is the images of the build labelled label, by component.
func testBuild(label string, comps ...string) map[string]string {
	images := map[string]string{}
	for _, c := range comps {
		images[c] = testImage(label + "/" + c)
	}
	return images
}

// testNVR is the NVR ART gives a build of image for version.
func testNVR(image, version string) string {
	return image + "-container-" + version + "-202610092112.p2.gec1e520.assembly.stream.el9"
}

// resolveNVR stores the resolved ART build, with nvr, of the image labelled
// label.
func resolveNVR(t *testing.T, srv *Server, label, nvr string) {
	t.Helper()
	b := artbuild.Build{Digest: testDigest(label), State: artbuild.StateResolved, NVR: nvr, CheckedAt: time.Now()}
	if err := srv.db.UpsertArtBuild(t.Context(), b); err != nil {
		t.Fatal(err)
	}
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
// 10 Oct 2026, when ART built 3.18.1: 3.18.1's STAGE build "stage", whose
// images ART staged the day before it assembled them, and its failed prod
// Release; the build "tested" and the periodic runs that tested it; 3.18.1's
// newest build "new", whose images are staged only for the builder; and the
// stream's newest build "mixed", of no version, as ART starts to build 3.18.2.
func seedQuay318(t *testing.T, srv *Server) {
	t.Helper()
	ctx := t.Context()
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	for _, v := range []string{"quay-v3.18.1", "quay-v3.18.2"} {
		if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: v, KonfluxApplication: "quay-3-18"}); err != nil {
			t.Fatal(err)
		}
	}
	assembly := func(name, env, created string) {
		t.Helper()
		if err := srv.db.UpsertStagedSnapshot(ctx, name, "3.18.1", "image", env, mustTime(t, created)); err != nil {
			t.Fatal(err)
		}
	}
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")

	staged := testBuild("stage", quay318...)
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-k2p4m", "2026-10-06T05:28:00Z", staged)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-q7x2v", "fbc-ri-stage-quay-3-18-quay-operator-k2p4m",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-06T05:28:00Z", "2026-10-06T05:40:00Z")
	seedImages(t, srv, "quay-stage-3-18-1-image-20261007200243", "2026-10-07T20:02:43Z", staged)
	assembly("quay-stage-3-18-1-image-20261007200243", "stage", "2026-10-07T20:02:43Z")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261007200243", "quay-stage-3-18-1-image-20261007200243",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-07T20:02:43Z", "2026-10-07T20:14:57Z")
	seedImages(t, srv, "quay-3-18-20261009-042610-000", "2026-10-09T04:29:06Z", testBuild("tested", all...))
	ri := testBuild("tested", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay")
	ri["quay-3-18-quay-builder"] = testImage("new/quay-3-18-quay-builder")
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-jxfbj", "2026-10-09T04:29:19Z", ri)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-r8fcb", "fbc-ri-stage-quay-3-18-quay-operator-jxfbj",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-09T04:29:20Z", "2026-10-09T04:41:33Z")
	seedImages(t, srv, "quay-prod-3-18-1-image-20261009133455", "2026-10-09T13:34:55Z", staged)
	assembly("quay-prod-3-18-1-image-20261009133455", "prod", "2026-10-09T13:34:55Z")
	seedKonfluxRelease(t, srv, "quay-prod-3-18-1-image-20261009133455", "quay-prod-3-18-1-image-20261009133455",
		"quay-advisory-prod-3-18", "Failed", "2026-10-09T13:34:55Z", "2026-10-09T13:39:03Z")

	seedImages(t, srv, "quay-3-18-20261010-013713-000", "2026-10-10T01:48:34Z", testBuild("new", all...))
	// Started later, but created first.
	seedImages(t, srv, "quay-3-18-20261010-013900-000", "2026-10-10T01:40:00Z", testBuild("other", all...))
	seedImages(t, srv, "quay-3-18-20261010-045000-000", "2026-10-10T05:00:00Z", testBuild("mixed", all...))
	// Newer, but not stream builds. 3.18.1's newer assembly failed stage.
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-d552l", "2026-10-10T01:49:22Z",
		testBuild("new", "quay-3-18-quay-builder", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay"))
	seedImages(t, srv, "quay-stage-3-18-1-image-20261010020000", "2026-10-10T02:00:00Z", staged)
	assembly("quay-stage-3-18-1-image-20261010020000", "stage", "2026-10-10T02:00:00Z")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261010020000", "quay-stage-3-18-1-image-20261010020000",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T02:00:00Z", "2026-10-10T02:05:00Z")
	seedImages(t, srv, "quay-prod-3-18-1-image-20261010030000", "2026-10-10T03:00:00Z", staged)
	assembly("quay-prod-3-18-1-image-20261010030000", "prod", "2026-10-10T03:00:00Z")

	for _, r := range []struct{ job, id, state, started string }{
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "5", "success", "2026-10-09T23:49:53Z"},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "6", "success", "2026-10-10T00:56:53Z"},
		{"azure-ocp422-e2e-install-azure-blob-nightly", "7", "failure", "2026-10-10T01:27:53Z"},
		{"aws-ocp422-e2e-install-aws-odf-nightly", "8", "success", "2026-10-10T03:11:53Z"},
	} {
		seedRun(t, srv, r.job, r.id, r.state, r.started, runImages("tested", "tested", "tested", "tested"))
	}

	// ART has resolved one image of most builds; the mixed build's operator is
	// already 3.18.2.
	for _, b := range []artbuild.Build{
		{Digest: testDigest("stage/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: "quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9", RecordID: "rec-stage-quay"},
		{Digest: testDigest("stage/quay-3-18-quay-clair"), State: artbuild.StateUnresolved},
		{Digest: testDigest("tested/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: testNVR("quay-quay", "3.18.1")},
		{Digest: testDigest("new/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: "quay-quay-container-3.18.1-202610092112.p2.gec1e520.assembly.stream.el9", RecordID: "rec-quay"},
		{Digest: testDigest("new/quay-3-18-quay-clair"), State: artbuild.StateUnresolved},
		{Digest: testDigest("other/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: testNVR("quay-quay", "3.18.1")},
		{Digest: testDigest("mixed/quay-3-18-quay-quay"), State: artbuild.StateResolved, NVR: testNVR("quay-quay", "3.18.1")},
		{Digest: testDigest("mixed/quay-3-18-quay-clair"), State: artbuild.StateResolved, NVR: testNVR("quay-clair", "3.18.1")},
		{Digest: testDigest("mixed/quay-3-18-quay-operator"), State: artbuild.StateResolved, NVR: testNVR("quay-operator", "3.18.2")},
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

// seedImages stores a quay-3-18 Snapshot of images, by component.
func seedImages(t *testing.T, srv *Server, name, created string, images map[string]string) {
	t.Helper()
	id, err := srv.db.CreateSnapshot(t.Context(), "quay-3-18", name, mustTime(t, created))
	if err != nil {
		t.Fatal(err)
	}
	for c, img := range images {
		if err := srv.db.CreateSnapshotComponent(t.Context(), id, c, img); err != nil {
			t.Fatal(err)
		}
	}
}

const jobPrefix = "periodic-ci-quay-quay-redhat-3.18-"

// runImages is what a run records of the quay, clair, operator and bundle
// images, each named by the build it is from.
func runImages(quay, clair, operator, bundle string) []prow.Image {
	return []prow.Image{
		{Role: "quay", Digest: testDigest(quay + "/quay-3-18-quay-quay")},
		{Role: "clair", Digest: testDigest(clair + "/quay-3-18-quay-clair")},
		{Role: "quay-operator", Digest: testDigest(operator + "/quay-3-18-quay-operator")},
		{Role: "quay-operator-bundle", Digest: testDigest(bundle + "/quay-3-18-quay-operator-bundle")},
	}
}

// seedRun stores a quay-3-18 periodic run; nil images is a run that
// published no tested-images.json.
func seedRun(t *testing.T, srv *Server, job, id, state, started string, images []prow.Image) {
	t.Helper()
	at := mustTime(t, started)
	run := &prow.Run{
		JobName: jobPrefix + job, BuildID: id, Application: "quay-3-18", State: state, StartedAt: &at,
		ProwURL: "https://prow.example/" + id, ArtifactState: prow.ArtifactPresent, FetchedAt: at, Images: images,
	}
	if images == nil {
		run.ArtifactState = prow.ArtifactMissing
	}
	if err := srv.db.UpsertProwRun(t.Context(), run); err != nil {
		t.Fatal(err)
	}
}

// ciJob is the CI job of a run seedRun stored.
func ciJob(t *testing.T, job, state, id, started string, runs int) model.CIJob {
	at := mustTime(t, started)
	return model.CIJob{JobName: jobPrefix + job, State: state, ProwURL: "https://prow.example/" + id, StartedAt: &at, Runs: runs}
}

// A build's version is the X.Y.Z the NVRs of its images ART resolved carry,
// base image left out.
func TestBuildVersion(t *testing.T) {
	const app = "quay-3-18"
	image := func(c string) model.SnapshotImage { return model.SnapshotImage{Name: c, Image: testImage("b/" + c)} }
	snap := &model.ReleaseSnapshot{Components: []model.SnapshotImage{
		image("quay-3-18-base-rhel9"), image("quay-3-18-quay-clair"), image("quay-3-18-quay-operator-bundle"), image("quay-3-18-quay-quay"),
	}}
	const (
		quay   = "quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9"
		bundle = "quay-operator-metadata-container-3.18.1.202610061637.p2.g35cf767.assembly.stream.el9-1"
	)
	for _, tc := range []struct {
		desc string
		nvrs map[string]string // by component
		want string
	}{
		{"an image and a bundle", map[string]string{"quay-3-18-quay-quay": quay, "quay-3-18-quay-operator-bundle": bundle}, "3.18.1"},
		{"unresolved images left out", map[string]string{"quay-3-18-quay-operator-bundle": bundle}, "3.18.1"},
		{"no image resolved", map[string]string{}, ""},
		{"two versions", map[string]string{"quay-3-18-quay-quay": quay, "quay-3-18-quay-clair": testNVR("quay-clair", "3.18.2")}, ""},
		{"3.18.10 is not 3.18.1", map[string]string{"quay-3-18-quay-quay": testNVR("quay-quay", "3.18.10")}, "3.18.10"},
		{"base image left out", map[string]string{"quay-3-18-quay-quay": quay, "quay-3-18-base-rhel9": testNVR("base-rhel9", "5.0.0")}, "3.18.1"},
		{"no version field", map[string]string{"quay-3-18-quay-quay": "quay-quay-container"}, ""},
	} {
		nvrs := map[string]string{}
		for c, nvr := range tc.nvrs {
			nvrs[testImage("b/"+c)] = nvr
		}
		if got := buildVersion(app, snap, nvrs); got != tc.want {
			t.Errorf("%s: version %q, want %q", tc.desc, got, tc.want)
		}
	}
	if !lowerVersion("3.18.9", "3.18.10") || lowerVersion("3.18.10", "3.18.9") || lowerVersion("3.18.1", "3.18.1") {
		t.Error("lowerVersion does not compare X.Y.Z numerically")
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

	// 3.18.1's release candidate is its STAGE build, and its newest build, not
	// the stream's newer mixed one, holds other images. The candidate has the
	// prod Release: the newest Release of a prod Snapshot, not the newest prod
	// Snapshot. No run installed its bundle.
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	c := got.Candidate
	if got.Shipped || got.Reason != "" || c == nil || c.Snapshot != "quay-stage-3-18-1-image-20261007200243" ||
		!c.CreatedAt.Equal(mustTime(t, "2026-10-07T20:02:43Z")) {
		t.Fatalf("3.18.1 candidate = %+v, reason %q", c, got.Reason)
	}
	if !slices.Equal(names(c.Components), quay318) || c.Components[0].Image != testImage("stage/"+quay318[0]) {
		t.Errorf("3.18.1 components = %+v", c.Components)
	}
	wantQuay := model.CandidateComponent{
		Name:     "quay-3-18-quay-quay",
		Image:    testImage("stage/quay-3-18-quay-quay"),
		NVR:      "quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9",
		BuildURL: "https://art.example/build?nvr=quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9&record_id=rec-stage-quay",
	}
	wantClair := model.CandidateComponent{Name: "quay-3-18-quay-clair", Image: testImage("stage/quay-3-18-quay-clair")}
	if c.Components[9] != wantQuay || c.Components[6] != wantClair {
		t.Errorf("3.18.1 quay, clair = %+v, %+v; want %+v, %+v", c.Components[9], c.Components[6], wantQuay, wantClair)
	}
	if p := c.Prod; p == nil || p.Name != "quay-prod-3-18-1-image-20261009133455" || p.ReleasedReason != "Failed" || p.FailedTask != "verify-conforma" {
		t.Errorf("3.18.1 prod = %+v", p)
	}
	if c.CI == nil || len(c.CI) != 0 {
		t.Errorf("3.18.1 ci = %+v, want none", c.CI)
	}
	// Its images reached stage the day before, but the build did with its
	// own Release.
	if s := c.Stage; s.State != "staged" || s.StagedAt == nil || !s.StagedAt.Equal(mustTime(t, "2026-10-07T20:14:57Z")) {
		t.Errorf("3.18.1 candidate stage = %+v", s)
	}
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-013713-000" || !n.CreatedAt.Equal(mustTime(t, "2026-10-10T01:48:34Z")) || n.Stage != "not_staged" {
		t.Errorf("3.18.1 newest = %+v, want quay-3-18-20261010-013713-000, not staged", n)
	}

	// 3.14.10 has no STAGE build, so no release candidate, but a newest build.
	// No STAGE Release of quay-3-14 at all: its stage cannot tell.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14"}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"),
		"quay-3-14-base-rhel8", "quay-3-14-quay-builder", "quay-3-14-quay-builder-qemu", "quay-3-14-quay-clair")
	resolveNVR(t, srv, "quay-3-14-20261009-185801-000/quay-3-14-quay-clair", testNVR("quay-clair", "3.14.10"))
	var raw map[string]any
	getJSON(t, srv, "/api/v1/releases/quay-v3.14.10/candidate", http.StatusOK, &raw)
	want := map[string]any{
		"shipped": false, "candidate": nil, "reason": "no release candidate yet",
		"newest": map[string]any{"snapshot": "quay-3-14-20261009-185801-000", "created_at": "2026-10-09T19:27:56Z", "stage": "unknown"},
	}
	if !reflect.DeepEqual(raw, want) {
		t.Errorf("3.14.10 = %v, want %v", raw, want)
	}
}

// A run tested the candidate when it installed the candidate's operator
// bundle, whatever other images it recorded.
func TestGetCandidateCI(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	for _, r := range []struct {
		job, id, state, started string
		images                  []prow.Image
	}{
		{"aws-ocp422-e2e-install-aws-s3-nightly", "1", "failure", "2026-10-08T00:56:53Z", runImages("tested", "tested", "tested", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "2", "success", "2026-10-08T12:56:53Z", runImages("new", "new", "new", "stage")},
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "3", "success", "2026-10-08T11:49:53Z", runImages("tested", "new", "tested", "stage")},
		// The candidate's quay, clair and operator beside another bundle.
		{"azure-ocp422-e2e-install-azure-blob-nightly", "4", "success", "2026-10-08T13:27:53Z", runImages("stage", "stage", "stage", "tested")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "9", "success", "2026-10-10T04:00:00Z", nil},
	} {
		seedRun(t, srv, r.job, r.id, r.state, r.started, r.images)
	}
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want := []model.CIJob{
		ciJob(t, "aws-ocp422-e2e-install-aws-s3-nightly", "success", "2", "2026-10-08T12:56:53Z", 2),
		ciJob(t, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "success", "3", "2026-10-08T11:49:53Z", 1),
	}
	if got.Candidate == nil {
		t.Fatal("3.18.1 has no candidate")
	}
	if ci := got.Candidate.CI; !reflect.DeepEqual(ci, want) {
		t.Errorf("3.18.1 ci = %+v, want %+v", ci, want)
	}
}

// The newest build shows beside the candidate when it holds another image of
// a component the candidate holds, staged or not by those images alone: like
// quay-3-17 for 3.17.6, the stream builds the CSO and QBO operators, which
// 3.18.2's STAGE build leaves out.
func TestGetCandidateNewest(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	operators := []string{"quay-3-18-container-security-operator", "quay-3-18-quay-bridge-operator"}
	stage := testBuild("stage2", slices.DeleteFunc(slices.Clone(quay318), func(c string) bool { return slices.Contains(operators, c) })...)
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", stage)
	if err := srv.db.UpsertStagedSnapshot(t.Context(), "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage", mustTime(t, "2026-10-10T04:00:00Z")); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-2-image-20261010040000", "quay-stage-3-18-2-image-20261010040000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	resolveNVR(t, srv, "stage2/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))

	// A stream build of the STAGE build's images and of the operators.
	same := maps.Clone(stage)
	maps.Copy(same, testBuild("same", operators...))
	seedImages(t, srv, "quay-3-18-20261010-060000-000", "2026-10-10T06:00:00Z", same)
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if got.Newest != nil {
		t.Errorf("3.18.2 newest = %+v, want none: it holds the STAGE build's images of their components", got.Newest)
	}

	// A newer one with another quay, staged on its own.
	next := maps.Clone(same)
	next["quay-3-18-quay-quay"] = testImage("next/quay-3-18-quay-quay")
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedImages(t, srv, "quay-3-18-20261010-070000-000", "2026-10-10T07:00:00Z", next)
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay", "2026-10-10T07:10:00Z", map[string]string{"quay-3-18-quay-quay": next["quay-3-18-quay-quay"]})
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay", "fbc-ri-stage-quay-3-18-quay",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T07:10:00Z", "2026-10-10T07:20:00Z")
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-070000-000" || n.Stage != "staged" {
		t.Errorf("3.18.2 newest = %+v, want quay-3-18-20261010-070000-000, staged", n)
	}
}

// A build of a version counts for it only if ART built no lower version
// after it: ART built 3.18.2 until 28 Sep, then went back to 3.18.1. Its next
// 3.18.2 build counts, and hides no older 3.18.1 build.
func TestGetCandidateVersionSwitch(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	ctx := t.Context()
	// 3.18.2's STAGE build gives it a candidate, beside which its newest
	// build shows.
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", testBuild("stage2", quay318...))
	if err := srv.db.UpsertStagedSnapshot(ctx, "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage", mustTime(t, "2026-10-10T04:00:00Z")); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-2-image-20261010040000", "quay-stage-3-18-2-image-20261010040000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	seedImages(t, srv, "quay-3-18-20260928-214539-000", "2026-09-28T21:57:16Z", testBuild("early", quay318...))
	resolveNVR(t, srv, "early/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if c := got.Candidate; c == nil || c.Snapshot != "quay-stage-3-18-2-image-20261010040000" {
		t.Fatalf("3.18.2 candidate = %+v, want its STAGE build", c)
	}
	if got.Newest != nil {
		t.Errorf("3.18.2 newest = %+v, want no 28 Sep build", got.Newest)
	}

	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", testBuild("next", quay318...))
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261011-000000-000" {
		t.Errorf("3.18.2 newest = %+v, want quay-3-18-20261011-000000-000", n)
	}
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-013713-000" {
		t.Errorf("3.18.1 newest = %+v, want quay-3-18-20261010-013713-000", n)
	}
}

// A shipped version has nothing to decide; a version ART cannot build, or
// that has no STAGE build, says why it has no candidate, and one with no build
// at all has no newest build either.
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
		return map[string]any{"shipped": shipped, "candidate": nil, "reason": reason, "newest": nil}
	}
	for version, want := range map[string]map[string]any{
		"quay-v3.18.0": none(true, ""), // released in JIRA
		"quay-v3.18.3": none(true, ""), // published in the catalog
		"omr-v2.0.13":  none(false, "not a concrete quay-vX.Y.Z version"),
		"3.16.3":       none(false, "no Konflux application"),
		"quay-v3.20.0": none(false, "no release candidate yet"),
		// Every quay-3-18 build is of 3.18.1, or of no version.
		"quay-v3.18.2": none(false, "no release candidate yet"),
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
			Digest: testDigest("stage/quay-3-18-quay-operator"), State: scan.StateScanned,
			PipelineRun: "quay-3-18-quay-operator-7mbwk", DetailURL: page + "quay-3-18-quay-operator-7mbwk",
			Counts:  &model.ScanCounts{Basis: scan.BasisArchFindings, NoFix: model.SeverityCounts{High: 24, Medium: 264, Low: 232}},
			Reports: map[string]string{"sha256:arch": "sha256:report"},
		},
		{
			Digest: testDigest("stage/quay-3-18-quay-quay"), State: scan.StateScanFailed,
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
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
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

// The scan sync reads the images of the release candidate, the STAGE build,
// of every version not archived, once each; a version whose candidate is its
// newest build, or that has no build, reads none.
func TestCandidateDigests(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	for _, v := range []string{"quay-v3.18.3", "quay-v3.18.4"} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: v, KonfluxApplication: "quay-3-18"}); err != nil {
			t.Fatal(err)
		}
	}
	// 3.18.2's STAGE build keeps 3.18.1's container-security-operator bundle.
	const kept = "quay-3-18-container-security-operator-bundle"
	stage2 := testBuild("stage2", quay318...)
	stage2[kept] = testImage("stage/" + kept)
	const snap = "quay-stage-3-18-2-image-20261010120000"
	seedImages(t, srv, snap, "2026-10-10T12:00:00Z", stage2)
	if err := srv.db.UpsertStagedSnapshot(t.Context(), snap, "3.18.2", "image", "stage", mustTime(t, "2026-10-10T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, snap, snap, "quay-advisory-stage-3-18", "Succeeded", "2026-10-10T12:00:00Z", "2026-10-10T12:12:00Z")
	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", testBuild("next", quay318...))
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.3"))

	got, err := srv.CandidateDigests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 3.18.1's and 3.18.2's STAGE builds: 3.18.3's candidate is its newest
	// build, and 3.18.4 has no build.
	var want []string
	for _, c := range quay318 {
		want = append(want, testDigest("stage/"+c))
		if c != kept {
			want = append(want, testDigest("stage2/"+c))
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("digests = %v, want %v", got, want)
	}

	for _, v := range []string{"quay-v3.18.1", "quay-v3.18.2"} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: v, KonfluxApplication: "quay-3-18", Archived: true}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := srv.CandidateDigests(t.Context()); err != nil || len(got) != 0 {
		t.Errorf("3.18.1 and 3.18.2 archived: digests = %v, %v; want none", got, err)
	}
}
