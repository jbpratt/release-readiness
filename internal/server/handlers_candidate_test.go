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
// newest build "new", whose images are staged only for both bundles and the
// builder; and the stream's newest build "mixed", of no version, as ART
// starts to build 3.18.2.
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

	newest := testBuild("new", all...)
	for _, c := range []string{"quay-3-18-container-security-operator-bundle", "quay-3-18-quay-bridge-operator-bundle"} {
		newest[c] = staged[c]
	}
	seedImages(t, srv, "quay-3-18-20261010-013713-000", "2026-10-10T01:48:34Z", newest)
	// Started later, but created first.
	seedImages(t, srv, "quay-3-18-20261010-013900-000", "2026-10-10T01:40:00Z", testBuild("other", all...))
	seedImages(t, srv, "quay-3-18-20261010-045000-000", "2026-10-10T05:00:00Z", testBuild("mixed", all...))
	// Newer, but not stream builds. 3.18.1's newer assembly failed stage.
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-d552l", "2026-10-10T01:49:22Z",
		testBuild("new", "quay-3-18-quay-builder", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay"))
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-ssrlx", "fbc-ri-stage-quay-3-18-quay-operator-d552l",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T01:49:22Z", "2026-10-10T01:52:21Z")
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

// rows is each build row's snapshot, creation time and roles.
func rows(builds []model.BuildRow) []string {
	out := []string{}
	for _, b := range builds {
		out = append(out, fmt.Sprintf("%s %s %v", b.Snapshot, b.CreatedAt.Format(time.RFC3339), b.Roles))
	}
	return out
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

	// 3.18.1 has a STAGE build, then its newest build, not the stream's newer
	// mixed one, and, as no run tested either, its newest build the periodics
	// tested. Only the STAGE build has the prod Release: the newest Release of
	// a prod Snapshot, not the newest prod Snapshot. Only the last tested
	// build has CI.
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	c := got.Candidate
	if got.Shipped || got.Reason != "" || c == nil || c.Snapshot != "quay-stage-3-18-1-image-20261007200243" || c.Source != "staged" ||
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
	b := got.Builds
	want := []string{
		"quay-stage-3-18-1-image-20261007200243 2026-10-07T20:02:43Z [candidate]",
		"quay-3-18-20261010-013713-000 2026-10-10T01:48:34Z [newest]",
		"quay-3-18-20261009-042610-000 2026-10-09T04:29:06Z [last_tested]",
	}
	if !slices.Equal(rows(b), want) {
		t.Fatalf("3.18.1 builds = %v, want %v", rows(b), want)
	}
	if p := b[0].Prod; p == nil || p.Name != "quay-prod-3-18-1-image-20261009133455" || p.ReleasedReason != "Failed" || p.FailedTask != "verify-conforma" {
		t.Errorf("3.18.1 prod = %+v", p)
	}
	if b[1].Prod != nil || b[2].Prod != nil || len(b[0].CI) != 0 || len(b[1].CI) != 0 || len(b[2].CI) == 0 {
		t.Errorf("3.18.1 prod %+v, %+v on the others; ci %+v, %+v, %+v", b[1].Prod, b[2].Prod, b[0].CI, b[1].CI, b[2].CI)
	}
	// Its images reached stage the day before, but the build did with its
	// own Release.
	if s := b[0].Stage; s.State != "staged" || s.StagedAt == nil || !s.StagedAt.Equal(mustTime(t, "2026-10-07T20:14:57Z")) {
		t.Errorf("3.18.1 candidate stage = %+v", s)
	}

	// 3.14.10 has none staged: its newest build. No STAGE Release of quay-3-14
	// at all: the flag cannot tell.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14"}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"),
		"quay-3-14-base-rhel8", "quay-3-14-quay-builder", "quay-3-14-quay-builder-qemu", "quay-3-14-quay-clair")
	resolveNVR(t, srv, "quay-3-14-20261009-185801-000/quay-3-14-quay-clair", testNVR("quay-clair", "3.14.10"))
	var raw map[string]any
	getJSON(t, srv, "/api/v1/releases/quay-v3.14.10/candidate", http.StatusOK, &raw)
	wantBuilds := []any{map[string]any{
		"snapshot": "quay-3-14-20261009-185801-000", "created_at": "2026-10-09T19:27:56Z", "roles": []any{"candidate", "newest"},
		"stage": map[string]any{
			"state": "unknown", "staged_at": nil, "total": float64(3), "not_staged": []any{}, "release": nil,
			"no_bundle": []any{}, "no_release": []any{},
		},
		"prod": nil, "ci": []any{},
	}}
	if cand, _ := raw["candidate"].(map[string]any); cand["source"] != "newest" || !reflect.DeepEqual(raw["builds"], wantBuilds) {
		t.Errorf("3.14.10 candidate source %v, builds = %v; want newest, %v", cand["source"], raw["builds"], wantBuilds)
	}
}

// Every build has its stage flag, which names the failed STAGE Release
// holding its unstaged images, tells which others wait on their bundle and,
// once all are staged, dates the build by the image that reached stage last,
// at its first time there.
func TestGetCandidateStage(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	f := got.Builds[1].Stage
	wantNotStaged := []string{
		"quay-3-18-container-security-operator",
		"quay-3-18-quay-bridge-operator",
		"quay-3-18-quay-builder-qemu",
		"quay-3-18-quay-clair",
		"quay-3-18-quay-operator",
		"quay-3-18-quay-operator-bundle",
		"quay-3-18-quay-quay",
	}
	// ssrlx holds five of them; the CSO and QBO operators are in no STAGE
	// Release, but their bundles are staged.
	wantNoBundle := []string{"quay-3-18-container-security-operator", "quay-3-18-quay-bridge-operator"}
	if f.State != "not_staged" || f.StagedAt != nil || f.Total != 10 || !slices.Equal(f.NotStaged, wantNotStaged) ||
		f.Release == nil || f.Release.Name != "fbc-ri-stage-quay-3-18-quay-operator-ssrlx" ||
		!slices.Equal(f.NoBundle, wantNoBundle) || len(f.NoRelease) != 0 {
		t.Fatalf("3.18.1 newest stage = %+v", f)
	}

	// ssrlx is retried, and the CSO and QBO operators reach stage, CSO twice.
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-ssrlx", "fbc-ri-stage-quay-3-18-quay-operator-d552l",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T01:49:22Z", "2026-10-10T02:30:00Z")
	operators := map[string]string{}
	for _, c := range []string{"quay-3-18-container-security-operator", "quay-3-18-quay-bridge-operator"} {
		operators[c] = testImage("new/" + c)
	}
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-operators", "2026-10-10T02:40:00Z", operators)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-operators", "fbc-ri-stage-quay-3-18-operators",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T02:40:00Z", "2026-10-10T02:50:00Z")
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-cso", "2026-10-10T03:20:00Z",
		map[string]string{"quay-3-18-container-security-operator": operators["quay-3-18-container-security-operator"]})
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-cso", "fbc-ri-stage-quay-3-18-cso",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T03:20:00Z", "2026-10-10T03:30:00Z")
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	if f := got.Builds[1].Stage; f.State != "staged" || f.Total != 10 || len(f.NotStaged) != 0 || f.Release != nil ||
		len(f.NoBundle) != 0 || len(f.NoRelease) != 0 || f.StagedAt == nil || !f.StagedAt.Equal(mustTime(t, "2026-10-10T02:50:00Z")) {
		t.Errorf("all staged: 3.18.1 newest stage = %+v, want staged at 02:50, when CSO and QBO first got there", f)
	}
}

// An image no STAGE Release holds waits on its bundle when the build's bundle
// for it, named as the page names components, is staged; otherwise it, like a
// bundle in no Release, is in none.
func TestStageFlagNoRelease(t *testing.T) {
	succeeded := func(images ...string) model.StageRelease {
		return model.StageRelease{KonfluxRelease: model.KonfluxRelease{ReleasedStatus: "True", ReleasedReason: "Succeeded"}, Images: images}
	}
	for _, tc := range []struct{ app, operator, bundle string }{
		{"quay-3-17", "quay-3-17-container-security-operator", "quay-3-17-quay-container-security-operator-bundle"},
		{"quay-3-18", "quay-3-18-quay-bridge-operator", "quay-3-18-quay-bridge-operator-bundle"},
		{"quay-3-15", "quay-3-15-container-security-operator", "container-security-operator-bundle"},
	} {
		images := []model.SnapshotImage{{Name: tc.operator, Image: testImage("op")}, {Name: tc.bundle, Image: testImage("bundle")}}
		f := stageFlag(tc.app, images, []model.StageRelease{succeeded(testImage("bundle"))})
		if !slices.Equal(f.NoBundle, []string{tc.operator}) || len(f.NoRelease) != 0 {
			t.Errorf("%s: no_bundle %v, no_release %v; want [%s], []", tc.app, f.NoBundle, f.NoRelease, tc.operator)
		}
	}

	images := []model.SnapshotImage{
		{Name: "quay-3-17-container-security-operator", Image: testImage("op")},
		{Name: "quay-3-17-quay-container-security-operator-bundle", Image: testImage("bundle")},
	}
	f := stageFlag("quay-3-17", images, []model.StageRelease{succeeded(testImage("other"))})
	want := []string{"quay-3-17-container-security-operator", "quay-3-17-quay-container-security-operator-bundle"}
	if len(f.NoBundle) != 0 || !slices.Equal(f.NoRelease, want) {
		t.Errorf("bundle not staged: no_bundle %v, no_release %v; want [], %v", f.NoBundle, f.NoRelease, want)
	}
}

// A run tested a build only when it recorded the build's quay, clair,
// operator and bundle digests. A tested candidate, or a tested newest build,
// leaves out the last tested build.
func TestGetCandidateCI(t *testing.T) {
	// No run tested 3.18.1's candidate or newest build; the newest run that
	// recorded all four images tested the last tested build.
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want := []model.CIJob{
		ciJob(t, "aws-ocp422-e2e-install-aws-odf-nightly", "success", "8", "2026-10-10T03:11:53Z", 1),
		ciJob(t, "aws-ocp422-e2e-install-aws-s3-nightly", "success", "6", "2026-10-10T00:56:53Z", 1),
		ciJob(t, "azure-ocp422-e2e-install-azure-blob-nightly", "failure", "7", "2026-10-10T01:27:53Z", 1),
		ciJob(t, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "success", "5", "2026-10-09T23:49:53Z", 1),
	}
	if b := got.Builds; len(b) != 3 || len(b[0].CI) != 0 || len(b[1].CI) != 0 || b[2].Snapshot != "quay-3-18-20261009-042610-000" || !reflect.DeepEqual(b[2].CI, want) {
		t.Errorf("3.18.1 builds = %v; want the last tested build with ci %+v", rows(b), want)
	}

	for _, r := range []struct {
		job, id, state, started string
		images                  []prow.Image
	}{
		{"aws-ocp422-e2e-install-aws-s3-nightly", "1", "failure", "2026-10-08T00:56:53Z", runImages("stage", "stage", "stage", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "2", "success", "2026-10-08T12:56:53Z", runImages("stage", "stage", "stage", "stage")},
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "3", "success", "2026-10-08T11:49:53Z", runImages("stage", "stage", "stage", "stage")},
		// The staged bundle beside another clair is another build.
		{"azure-ocp422-e2e-install-azure-blob-nightly", "4", "success", "2026-10-08T13:27:53Z", runImages("stage", "tested", "stage", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "9", "success", "2026-10-10T04:00:00Z", nil},
	} {
		seedRun(t, srv, r.job, r.id, r.state, r.started, r.images)
	}
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want = []model.CIJob{
		ciJob(t, "aws-ocp422-e2e-install-aws-s3-nightly", "success", "2", "2026-10-08T12:56:53Z", 2),
		ciJob(t, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "success", "3", "2026-10-08T11:49:53Z", 1),
	}
	if b := got.Builds; len(b) != 2 || !reflect.DeepEqual(b[0].CI, want) || len(b[1].CI) != 0 {
		t.Errorf("3.18.1 builds = %v, candidate ci %+v; want no last tested build and ci %+v", rows(b), b[0].CI, want)
	}

	// A run of 3.18.1's newest build, but none of its candidate.
	srv = setupTestServer(t)
	seedQuay318(t, srv)
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly-fips", "11", "failure", "2026-10-10T17:21:25Z", runImages("new", "new", "new", "new"))
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want = []model.CIJob{ciJob(t, "aws-ocp422-e2e-install-aws-s3-nightly-fips", "failure", "11", "2026-10-10T17:21:25Z", 1)}
	if b := got.Builds; len(b) != 2 || len(b[0].CI) != 0 || !reflect.DeepEqual(b[1].CI, want) {
		t.Errorf("3.18.1 builds = %v; want no last tested build and newest ci %+v", rows(b), want)
	}
}

// The last tested build is the version's newest build holding the four
// images a run recorded, never an fbc-ri-* or ART assembly Snapshot holding
// them; a run whose build no build of the version holds gives way to the
// next run's build.
func TestGetCandidateLastTested(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	tested := map[string]string{}
	for _, c := range []string{"quay-3-18-quay-quay", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle"} {
		tested[c] = testImage("tested/" + c)
	}
	// Beside the seed's newer fbc-ri-stage Snapshot of the tested build: an
	// older stream build and a newer assembly of it, and a newer stream build
	// with all but its clair.
	seedImages(t, srv, "quay-3-18-20261008-120000-000", "2026-10-08T12:00:00Z", tested)
	seedImages(t, srv, "quay-stage-3-18-1-image-20261009050000", "2026-10-09T05:00:00Z", tested)
	if err := srv.db.UpsertStagedSnapshot(t.Context(), "quay-stage-3-18-1-image-20261009050000", "3.18.1", "image", "stage", mustTime(t, "2026-10-09T05:00:00Z")); err != nil {
		t.Fatal(err)
	}
	partial := maps.Clone(tested)
	partial["quay-3-18-quay-clair"] = testImage("other/quay-3-18-quay-clair")
	seedImages(t, srv, "quay-3-18-20261009-200000-000", "2026-10-09T20:00:00Z", partial)
	// The newest run tested the stream's mixed build, which is of no version.
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "10", "success", "2026-10-10T05:30:00Z", runImages("mixed", "mixed", "mixed", "mixed"))

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	want := []string{
		"quay-stage-3-18-1-image-20261007200243 2026-10-07T20:02:43Z [candidate]",
		"quay-3-18-20261010-013713-000 2026-10-10T01:48:34Z [newest]",
		"quay-3-18-20261009-042610-000 2026-10-09T04:29:06Z [last_tested]",
	}
	if !slices.Equal(rows(got.Builds), want) {
		t.Errorf("3.18.1 builds = %v, want %v", rows(got.Builds), want)
	}
}

// A build of a version counts for it only if ART built no lower version
// after it: ART built 3.18.2 until 28 Sep, then went back to 3.18.1. Its next
// 3.18.2 build counts, and hides no older 3.18.1 build.
func TestGetCandidateVersionSwitch(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	ctx := t.Context()
	// 3.18.2's STAGE build gives it a candidate, so the rest of its builds
	// are listed.
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", testBuild("stage2", quay318...))
	if err := srv.db.UpsertStagedSnapshot(ctx, "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage", mustTime(t, "2026-10-10T04:00:00Z")); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-2-image-20261010040000", "quay-stage-3-18-2-image-20261010040000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	seedImages(t, srv, "quay-3-18-20260928-214539-000", "2026-09-28T21:57:16Z", testBuild("early", quay318...))
	resolveNVR(t, srv, "early/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "12", "success", "2026-09-29T00:00:00Z", runImages("early", "early", "early", "early"))

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	want := []string{"quay-stage-3-18-2-image-20261010040000 2026-10-10T04:00:00Z [candidate]"}
	if !slices.Equal(rows(got.Builds), want) {
		t.Errorf("3.18.2 builds = %v, want %v: no newest or last tested 28 Sep build", rows(got.Builds), want)
	}

	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", testBuild("next", quay318...))
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	want = append(want, "quay-3-18-20261011-000000-000 2026-10-11T00:00:00Z [newest]")
	if !slices.Equal(rows(got.Builds), want) {
		t.Errorf("3.18.2 builds = %v, want %v", rows(got.Builds), want)
	}
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	if b := got.Builds; len(b) < 2 || b[1].Snapshot != "quay-3-18-20261010-013713-000" {
		t.Errorf("3.18.1 builds = %v, want newest quay-3-18-20261010-013713-000", rows(b))
	}
}

// A shipped version has nothing to decide; a version ART cannot build, or
// has not built yet, says why it has no candidate.
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
		return map[string]any{"shipped": shipped, "candidate": nil, "reason": reason, "builds": []any{}}
	}
	for version, want := range map[string]map[string]any{
		"quay-v3.18.0": none(true, ""), // released in JIRA
		"quay-v3.18.3": none(true, ""), // published in the catalog
		"omr-v2.0.13":  none(false, "not a concrete quay-vX.Y.Z version"),
		"3.16.3":       none(false, "no Konflux application"),
		"quay-v3.20.0": none(false, "no build of 3.20.0 yet"),
		// Every quay-3-18 build is of 3.18.1, or of no version.
		"quay-v3.18.2": none(false, "no build of 3.18.2 yet"),
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

// The scan sync reads the candidate images of every version not archived,
// once each; a version with no build reads none.
func TestCandidateDigests(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14", Archived: true}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"), "quay-3-14-quay-clair")
	resolveNVR(t, srv, "quay-3-14-20261009-185801-000/quay-3-14-quay-clair", testNVR("quay-clair", "3.14.10"))
	// 3.18.3's first build keeps 3.18.1's STAGE bundles.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.3", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	shared := []string{"quay-3-18-container-security-operator-bundle", "quay-3-18-quay-bridge-operator-bundle"}
	next := testBuild("next", quay318...)
	for _, c := range shared {
		next[c] = testImage("stage/" + c)
	}
	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", next)
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.3"))

	got, err := srv.CandidateDigests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 3.18.1's STAGE build, and 3.18.3's newest build less the two bundles it
	// shares with it. 3.18.2 has no build.
	var want []string
	for _, c := range quay318 {
		want = append(want, testDigest("stage/"+c))
		if !slices.Contains(shared, c) {
			want = append(want, testDigest("next/"+c))
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("digests = %v, want %v", got, want)
	}
}
