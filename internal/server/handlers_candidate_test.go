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
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
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

// seedFBC stores the resolved FBC image of nvr with its FBC record: its ART
// build ended at end, holding bundles. It returns the image's pullspec.
func seedFBC(t *testing.T, srv *Server, nvr, end string, bundles ...string) string {
	t.Helper()
	resolveNVR(t, srv, "fbc/"+nvr, nvr)
	r := artbuild.FBCRecord{Digest: testDigest("fbc/" + nvr), Pullspec: testImage("fbc/" + nvr), EndTime: end, BundleNVRs: bundles}
	if err := srv.db.UpsertFBCRecord(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	return r.Pullspec
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
	assembly := func(name, env string) {
		t.Helper()
		if err := srv.db.UpsertStagedSnapshot(ctx, name, "3.18.1", "image", env); err != nil {
			t.Fatal(err)
		}
	}
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")

	staged := testBuild("stage", quay318...)
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-k2p4m", "2026-10-06T05:28:00Z", staged)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-q7x2v", "fbc-ri-stage-quay-3-18-quay-operator-k2p4m",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-06T05:28:00Z", "2026-10-06T05:40:00Z")
	seedImages(t, srv, "quay-stage-3-18-1-image-20261007200243", "2026-10-07T20:02:43Z", staged)
	assembly("quay-stage-3-18-1-image-20261007200243", "stage")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261007200243", "quay-stage-3-18-1-image-20261007200243",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-07T20:02:43Z", "2026-10-07T20:14:57Z")
	seedImages(t, srv, "quay-3-18-20261009-042610-000", "2026-10-09T04:29:06Z", testBuild("tested", all...))
	ri := testBuild("tested", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay")
	ri["quay-3-18-quay-builder"] = testImage("new/quay-3-18-quay-builder")
	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-jxfbj", "2026-10-09T04:29:19Z", ri)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-r8fcb", "fbc-ri-stage-quay-3-18-quay-operator-jxfbj",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-09T04:29:20Z", "2026-10-09T04:41:33Z")
	seedImages(t, srv, "quay-prod-3-18-1-image-20261009133455", "2026-10-09T13:34:55Z", staged)
	assembly("quay-prod-3-18-1-image-20261009133455", "prod")
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
	assembly("quay-stage-3-18-1-image-20261010020000", "stage")
	seedKonfluxRelease(t, srv, "quay-stage-3-18-1-image-20261010020000", "quay-stage-3-18-1-image-20261010020000",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T02:00:00Z", "2026-10-10T02:05:00Z")
	seedImages(t, srv, "quay-prod-3-18-1-image-20261010030000", "2026-10-10T03:00:00Z", staged)
	assembly("quay-prod-3-18-1-image-20261010030000", "prod")

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

// olmImages is what a run records from olm-csvs.yaml: the operator, quay,
// clair, builder and builder-qemu images, each named by the build it is from.
func olmImages(operator, quay, clair, builder, qemu string) []prow.Image {
	return []prow.Image{
		{Role: "quay-operator", Digest: testDigest(operator + "/quay-3-18-quay-operator")},
		{Role: "quay", Digest: testDigest(quay + "/quay-3-18-quay-quay")},
		{Role: "clair", Digest: testDigest(clair + "/quay-3-18-quay-clair")},
		{Role: "quay-builder", Digest: testDigest(builder + "/quay-3-18-quay-builder")},
		{Role: "quay-builder-qemu", Digest: testDigest(qemu + "/quay-3-18-quay-builder-qemu")},
	}
}

// ciRows is each CI build row as "kind[ snapshot created_at[ state release]]:
// job=run,run ...", with jobs and run IDs as seedRun stored them, and
// "(install_failed)" after a run whose install failed.
func ciRows(builds []model.CIBuild) []string {
	rows := []string{}
	for _, b := range builds {
		row := b.Kind
		if b.CreatedAt != nil {
			row += " " + b.Snapshot + " " + b.CreatedAt.Format(time.RFC3339)
		}
		if b.CI != nil {
			row += " " + b.CI.State
			if b.CI.Release != nil {
				row += " " + b.CI.Release.Name
			}
		}
		row += ":"
		for _, j := range b.Jobs {
			var ids []string
			for _, r := range j.Runs {
				id := strings.TrimPrefix(r.ProwURL, "https://prow.example/")
				if r.InstallFailed {
					id += "(install_failed)"
				}
				ids = append(ids, id)
			}
			row += " " + strings.TrimPrefix(j.JobName, jobPrefix) + "=" + strings.Join(ids, ",")
		}
		rows = append(rows, row)
	}
	return rows
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
	if compareVersions("3.18.9", "3.18.10") >= 0 || compareVersions("3.18.10", "3.18.9") <= 0 || compareVersions("3.18.1", "3.18.1") != 0 {
		t.Error("compareVersions does not compare X.Y.Z numerically")
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
	// the stream's newer mixed one, holds other images and is not in CI. The
	// candidate has the prod Release: the newest Release of a prod Snapshot,
	// not the newest prod Snapshot. No run installed its bundle.
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	c := got.Candidate
	if got.Shipped || got.Reason != "" || c == nil || c.Snapshot != "quay-stage-3-18-1-image-20261007200243" ||
		!c.CreatedAt.Equal(mustTime(t, "2026-10-07T20:02:43Z")) {
		t.Fatalf("3.18.1 candidate = %+v, reason %q", c, got.Reason)
	}
	if !slices.Equal(names(c.Components), quay318) {
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
	if ci := got.CI; ci == nil || len(ci.Builds) == 0 || ciRows(ci.Builds)[0] != "candidate quay-stage-3-18-1-image-20261007200243 2026-10-07T20:02:43Z:" {
		t.Errorf("3.18.1 ci = %+v, want the candidate first, without runs", ci)
	}
	// Its images reached stage the day before, but the build did with its
	// own Release.
	if s := c.Stage; s.StagedAt == nil || !s.StagedAt.Equal(mustTime(t, "2026-10-07T20:14:57Z")) {
		t.Errorf("3.18.1 candidate stage = %+v", s)
	}
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-013713-000" || !n.CreatedAt.Equal(mustTime(t, "2026-10-10T01:48:34Z")) || n.CI != (model.CIState{State: "none"}) {
		t.Errorf("3.18.1 newest = %+v, want quay-3-18-20261010-013713-000, not in CI", n)
	}

	// 3.14.10 has no STAGE build, so no release candidate, but a newest build,
	// and CI since it was built, as it holds no bundle: no run, nothing to
	// pick. No per-build stage Release of quay-3-14 at all: whether it is in
	// CI cannot tell. Its images, base image left out, show with their ART
	// builds, if any.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14"}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"),
		"quay-3-14-base-rhel8", "quay-3-14-quay-builder", "quay-3-14-quay-builder-qemu", "quay-3-14-quay-clair")
	resolveNVR(t, srv, "quay-3-14-20261009-185801-000/quay-3-14-quay-clair", testNVR("quay-clair", "3.14.10"))
	component := func(name, nvr, url string) map[string]any {
		return map[string]any{"name": name, "image": testImage("quay-3-14-20261009-185801-000/" + name), "nvr": nvr, "build_url": url}
	}
	clair := testNVR("quay-clair", "3.14.10")
	var raw map[string]any
	getJSON(t, srv, "/api/v1/releases/quay-v3.14.10/candidate", http.StatusOK, &raw)
	want := map[string]any{
		"shipped": false, "candidate": nil, "reason": "no release candidate yet", "catalogs": nil, "unshipped_builds": nil,
		"fbc_candidate": nil, "fbc_stream": nil,
		"ci": map[string]any{"builds": []any{}, "latest_in_ci": nil, "passed": nil, "fbc": nil, "refresh": "none"},
		"newest": map[string]any{
			"snapshot": "quay-3-14-20261009-185801-000", "created_at": "2026-10-09T19:27:56Z",
			"ci": map[string]any{"state": "unknown", "release": nil},
			"components": []any{
				component("quay-3-14-quay-builder", "", ""),
				component("quay-3-14-quay-builder-qemu", "", ""),
				component("quay-3-14-quay-clair", clair, "https://art.example/build?nvr="+clair+"&record_id="),
			},
		},
	}
	if !reflect.DeepEqual(raw, want) {
		t.Errorf("3.14.10 = %v, want %v", raw, want)
	}
}

// The periodic runs since the stream first built the candidate's operator
// bundle, by the build each installed: the candidate, by its bundle or, for a
// run that recorded none, its five Quay images; each other build, newest
// first, with whether it is in CI; and an unknown build. A run that installed
// no Quay is left out.
func TestGetCandidateCI(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")
	seedImages(t, srv, "quay-3-18-20261006-052000-000", "2026-10-06T05:20:00Z", testBuild("stage", all...))
	// A newer build pairs the tested build's images with the candidate's older
	// bundle: the tested build, whose bundle is newer, is still the one in CI.
	paired := testBuild("tested", all...)
	paired["quay-3-18-quay-operator-bundle"] = testImage("stage/quay-3-18-quay-operator-bundle")
	seedImages(t, srv, "quay-3-18-20261009-224312-000", "2026-10-09T22:52:29Z", paired)
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-ssrlx", "fbc-ri-stage-quay-3-18-quay-operator-d552l",
		"quay-advisory-stage-3-18", "Failed", "2026-10-10T01:49:22Z", "2026-10-10T02:01:00Z")
	for _, r := range []struct {
		job, id, state, started string
		images                  []prow.Image
	}{
		// Before the stream built the candidate's bundle.
		{"aws-ocp422-e2e-install-aws-s3-nightly", "10", "success", "2026-10-06T00:56:55Z", runImages("stage", "stage", "stage", "stage")},
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "11", "failure", "2026-10-06T23:49:56Z", olmImages("stage", "stage", "stage", "stage", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "12", "failure", "2026-10-07T00:56:56Z", olmImages("stage", "stage", "stage", "stage", "stage")},
		// The candidate's operator, quay and clair beside another builder.
		{"azure-ocp422-e2e-install-azure-blob-nightly", "13", "failure", "2026-10-07T01:27:56Z", olmImages("stage", "stage", "stage", "tested", "stage")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "14", "success", "2026-10-08T00:56:37Z", runImages("stage", "stage", "stage", "stage")},
		// A bundle no stream build holds.
		{"gcp-ocp422-e2e-install-gcp-gcs-nightly", "15", "failure", "2026-10-08T23:49:38Z", runImages("orphan", "orphan", "orphan", "orphan")},
		{"aws-ocp422-e2e-install-aws-odf-nightly", "16", "failure", "2026-10-09T03:11:39Z", nil},
		{"aws-ocp422-e2e-install-aws-s3-nightly-fips", "17", "failure", "2026-10-10T17:21:25Z", runImages("other", "other", "other", "other")},
		{"aws-ocp422-e2e-install-aws-s3-nightly", "18", "failure", "2026-10-11T00:56:03Z", runImages("new", "new", "new", "new")},
	} {
		seedRun(t, srv, r.job, r.id, r.state, r.started, r.images)
	}

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	ci := got.CI
	if ci == nil {
		t.Fatal("3.18.1 has no CI")
	}
	want := []string{
		"candidate quay-stage-3-18-1-image-20261007200243 2026-10-07T20:02:43Z: aws-ocp422-e2e-install-aws-s3-nightly=14,12 gcp-ocp422-e2e-install-gcp-gcs-nightly=11",
		"build quay-3-18-20261010-013713-000 2026-10-10T01:48:34Z failed fbc-ri-stage-quay-3-18-quay-operator-ssrlx: aws-ocp422-e2e-install-aws-s3-nightly=18",
		"build quay-3-18-20261010-013900-000 2026-10-10T01:40:00Z none: aws-ocp422-e2e-install-aws-s3-nightly-fips=17",
		"build quay-3-18-20261009-042610-000 2026-10-09T04:29:06Z in fbc-ri-stage-quay-3-18-quay-operator-r8fcb: " +
			"aws-ocp422-e2e-install-aws-odf-nightly=8 aws-ocp422-e2e-install-aws-s3-nightly=6 azure-ocp422-e2e-install-azure-blob-nightly=7 gcp-ocp422-e2e-install-gcp-gcs-nightly=5",
		"unknown: azure-ocp422-e2e-install-azure-blob-nightly=13 gcp-ocp422-e2e-install-gcp-gcs-nightly=15",
	}
	if rows := ciRows(ci.Builds); !slices.Equal(rows, want) {
		t.Fatalf("builds:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	if r := ci.Builds[0].Jobs[0].Runs[0]; r.State != "success" || !r.StartedAt.Equal(mustTime(t, "2026-10-08T00:56:37Z")) || r.ProwURL != "https://prow.example/14" {
		t.Errorf("the candidate's newest run = %+v", r)
	}
	// The newest build failed to reach CI; the one before passed in it, so it
	// is the build to re-run release-from-fbc with, though a prod Release of
	// 3.18.1 was attempted.
	if l := ci.LatestInCI; l == nil || l.Snapshot != "quay-3-18-20261009-042610-000" || !l.CreatedAt.Equal(mustTime(t, "2026-10-09T04:29:06Z")) ||
		ci.Passed == nil || ci.Passed.Snapshot != l.Snapshot || ci.Refresh != "rerun" {
		t.Errorf("latest in CI = %+v, passed %+v, refresh %q; want both quay-3-18-20261009-042610-000, rerun", l, ci.Passed, ci.Refresh)
	}
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-013713-000" || n.CI.State != "failed" || n.CI.Release == nil || n.CI.Release.Name != "fbc-ri-stage-quay-3-18-quay-operator-ssrlx" {
		t.Errorf("newest = %+v, want quay-3-18-20261010-013713-000, failed to reach CI", n)
	}
}

// A run that installed no Quay shows only when its Quay install failed: on the
// build whose bundle the FBC of its catalog holds, when ART's record of that
// FBC lists the bundle of exactly one of the stream's builds. A run whose
// install failed says so on any row.
func TestGetCandidateInstallFailed(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	bundle := func(built string) string {
		return "quay-operator-metadata-container-3.18.1." + built + ".p2.g35cf767.assembly.stream.el9-1"
	}
	tested, newer := bundle("202610090339"), bundle("202610100137")
	resolveNVR(t, srv, "tested/quay-3-18-quay-operator-bundle", tested)
	resolveNVR(t, srv, "new/quay-3-18-quay-operator-bundle", newer)
	const fbc = "quay-operator-fbc-3.18.1-20261009044841.ocp4.22"
	for _, r := range []struct {
		job, id, started string
		installFailed    bool
		images           []prow.Image
	}{
		// Failed before OLM installed a CSV.
		{"aws-ocp422-e2e-install-aws-s3-nightly", "40", "2026-10-11T00:56:03Z", true, nil},
		// Died before the install, with the same catalog.
		{"azure-ocp422-e2e-install-azure-blob-nightly", "41", "2026-10-11T01:27:00Z", false, nil},
		// Failed after OLM installed the new build's CSV.
		{"aws-ocp422-e2e-install-aws-odf-nightly", "42", "2026-10-11T03:11:00Z", true, runImages("new", "new", "new", "new")},
	} {
		at := mustTime(t, r.started)
		run := &prow.Run{
			JobName: jobPrefix + r.job, BuildID: r.id, Application: "quay-3-18", State: "failure", StartedAt: &at,
			ProwURL: "https://prow.example/" + r.id, ArtifactState: prow.ArtifactPresent, CatalogRef: testImage("fbc/" + fbc),
			InstallFailed: r.installFailed, FetchedAt: at,
			Images: r.images,
		}
		if err := srv.db.UpsertProwRun(t.Context(), run); err != nil {
			t.Fatal(err)
		}
	}
	want := func(awsS3 string) []string {
		return []string{
			"candidate quay-stage-3-18-1-image-20261007200243 2026-10-07T20:02:43Z:",
			"build quay-3-18-20261010-013713-000 2026-10-10T01:48:34Z none: aws-ocp422-e2e-install-aws-odf-nightly=42(install_failed)",
			"build quay-3-18-20261009-042610-000 2026-10-09T04:29:06Z in fbc-ri-stage-quay-3-18-quay-operator-r8fcb: aws-ocp422-e2e-install-aws-odf-nightly=8 " +
				"aws-ocp422-e2e-install-aws-s3-nightly=" + awsS3 + " azure-ocp422-e2e-install-azure-blob-nightly=7 gcp-ocp422-e2e-install-gcp-gcs-nightly=5",
		}
	}
	for _, step := range []struct {
		desc    string
		bundles []string // the FBC record's; nil before ART's record is read
		awsS3   string   // the tested build's aws-s3 runs
	}{
		{"no FBC record", nil, "6"},
		{"the FBC holds the tested build's bundle", []string{tested}, "40(install_failed),6"},
		{"the FBC holds two builds' bundles", []string{tested, newer}, "6"},
	} {
		if step.bundles != nil {
			seedFBC(t, srv, fbc, "2026-10-09T05:42:05", step.bundles...)
		}
		var got model.ReleaseCandidate
		getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
		if rows := ciRows(got.CI.Builds); !slices.Equal(rows, want(step.awsS3)) {
			t.Errorf("%s: builds:\n%s\nwant:\n%s", step.desc, strings.Join(rows, "\n"), strings.Join(want(step.awsS3), "\n"))
		}
	}

	// Only a run whose install failed carries install_failed.
	var raw struct {
		CI struct {
			Builds []struct {
				Jobs []struct {
					Runs []map[string]any `json:"runs"`
				} `json:"jobs"`
			} `json:"builds"`
		} `json:"ci"`
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &raw)
	if b := raw.CI.Builds; len(b) != 3 {
		t.Fatalf("builds = %v", b)
	}
	failed, passed := raw.CI.Builds[1].Jobs[0].Runs[0], raw.CI.Builds[2].Jobs[0].Runs[0]
	if _, ok := passed["install_failed"]; ok || failed["install_failed"] != true {
		t.Errorf("runs 42, 8 = %v, %v; want install_failed true, and none", failed, passed)
	}
}

// With a candidate, CI names the newest build of its version, newer than the
// candidate, that a run passed with, to re-run release-from-fbc with, also
// once a prod Release was attempted; until one passes, it waits on a newer
// build in CI. A build in CI that holds the candidate's bundle is the
// candidate, and a run that passed with an older build than the candidate
// names none.
func TestGetCandidateRefresh(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")
	ri := []string{"quay-3-18-quay-builder", "quay-3-18-quay-builder-qemu", "quay-3-18-quay-clair", "quay-3-18-quay-operator", "quay-3-18-quay-operator-bundle", "quay-3-18-quay-quay"}
	inCI := func(build, snapshot, release, created, completed string) {
		t.Helper()
		seedImages(t, srv, snapshot, created, testBuild(build, ri...))
		seedKonfluxRelease(t, srv, release, snapshot, "quay-advisory-stage-3-18", "Succeeded", created, completed)
	}
	// 3.18.2's STAGE build, of a stream build in CI.
	seedImages(t, srv, "quay-3-18-20261010-035000-000", "2026-10-10T03:50:00Z", testBuild("stage2", all...))
	resolveNVR(t, srv, "stage2/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	inCI("stage2", "fbc-ri-stage-quay-3-18-quay-operator-x2b9k", "fbc-ri-stage-quay-3-18-quay-operator-m4q7z", "2026-10-10T03:51:00Z", "2026-10-10T03:59:00Z")
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", testBuild("stage2", quay318...))
	if err := srv.db.UpsertStagedSnapshot(t.Context(), "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage"); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-2-image-20261010040000", "quay-stage-3-18-2-image-20261010040000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	get := func() model.ReleaseCandidate {
		t.Helper()
		var got model.ReleaseCandidate
		getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
		if got.Candidate == nil || got.CI == nil || got.Newest != nil {
			t.Fatalf("3.18.2 candidate = %+v, ci %+v, newest %+v; want both, and no newest build beside the one CI names", got.Candidate, got.CI, got.Newest)
		}
		return got
	}
	// An older build of 3.18.2 passed CI after the stream built the
	// candidate's bundle.
	seedImages(t, srv, "quay-3-18-20261010-030000-000", "2026-10-10T03:00:00Z", testBuild("old", all...))
	resolveNVR(t, srv, "old/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "20", "success", "2026-10-10T04:56:00Z", runImages("old", "old", "old", "old"))
	if ci := get().CI; ci.LatestInCI != nil || ci.Passed != nil || ci.Refresh != "current" {
		t.Errorf("the candidate in CI: latest %+v, passed %+v, refresh %q; want none, none, current", ci.LatestInCI, ci.Passed, ci.Refresh)
	}

	// A newer build reaches CI, and its run fails.
	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", testBuild("next", all...))
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	inCI("next", "fbc-ri-stage-quay-3-18-quay-operator-c7d4w", "fbc-ri-stage-quay-3-18-quay-operator-p9t2h", "2026-10-11T00:01:00Z", "2026-10-11T00:12:00Z")
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "21", "failure", "2026-10-11T00:56:03Z", runImages("next", "next", "next", "next"))
	if ci := get().CI; ci.LatestInCI == nil || ci.LatestInCI.Snapshot != "quay-3-18-20261011-000000-000" || ci.Passed != nil || ci.Refresh != "waiting" {
		t.Errorf("a newer build failed CI: latest %+v, passed %+v, refresh %q; want quay-3-18-20261011-000000-000, none, waiting", ci.LatestInCI, ci.Passed, ci.Refresh)
	}

	// A run of it passes.
	seedRun(t, srv, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "22", "success", "2026-10-11T01:49:00Z", runImages("next", "next", "next", "next"))
	if ci := get().CI; ci.Passed == nil || ci.Passed.Snapshot != "quay-3-18-20261011-000000-000" || ci.Refresh != "rerun" {
		t.Errorf("a newer build passed CI: passed %+v, refresh %q; want quay-3-18-20261011-000000-000, rerun", ci.Passed, ci.Refresh)
	}

	// A newer build still reaches CI, untested.
	seedImages(t, srv, "quay-3-18-20261012-000000-000", "2026-10-12T00:00:00Z", testBuild("last", all...))
	resolveNVR(t, srv, "last/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	inCI("last", "fbc-ri-stage-quay-3-18-quay-operator-h3n8q", "fbc-ri-stage-quay-3-18-quay-operator-w5k2r", "2026-10-12T00:01:00Z", "2026-10-12T00:12:00Z")
	if ci := get().CI; ci.LatestInCI == nil || ci.LatestInCI.Snapshot != "quay-3-18-20261012-000000-000" ||
		ci.Passed == nil || ci.Passed.Snapshot != "quay-3-18-20261011-000000-000" || ci.Refresh != "rerun" {
		t.Errorf("a newer build in CI, untested: latest %+v, passed %+v, refresh %q; want quay-3-18-20261012-000000-000, quay-3-18-20261011-000000-000, rerun",
			ci.LatestInCI, ci.Passed, ci.Refresh)
	}

	// A prod Release of 3.18.2 is attempted.
	const prod = "quay-prod-3-18-2-image-20261012010000"
	seedImages(t, srv, prod, "2026-10-12T01:00:00Z", testBuild("stage2", quay318...))
	if err := srv.db.UpsertStagedSnapshot(t.Context(), prod, "3.18.2", "image", "prod"); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, prod, prod, "quay-advisory-prod-3-18", "Failed", "2026-10-12T01:00:00Z", "2026-10-12T01:05:00Z")
	if got := get(); got.Candidate.Prod == nil || got.CI.Refresh != "rerun" {
		t.Errorf("a prod Release attempted: prod %+v, refresh %q; want one, rerun", got.Candidate.Prod, got.CI.Refresh)
	}
}

// Without a candidate, CI holds the runs since the stream first built a bundle
// of the version's builds, by the build each installed, and names the newest
// of those bundles a run passed with, to pick as the candidate by running
// release-from-fbc with it. The newest build shows unless it is that one.
func TestGetCandidatePick(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	all := append(slices.Clone(quay318), "quay-3-18-base-rhel9")
	// 3.18.2's first build pairs its images with the bundle of the stream's
	// mixed build before it.
	first := testBuild("p1", all...)
	first["quay-3-18-quay-operator-bundle"] = testImage("mixed/quay-3-18-quay-operator-bundle")
	seedImages(t, srv, "quay-3-18-20261010-060000-000", "2026-10-10T06:00:00Z", first)
	resolveNVR(t, srv, "p1/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedImages(t, srv, "quay-3-18-20261010-070000-000", "2026-10-10T07:00:00Z", testBuild("p2", all...))
	resolveNVR(t, srv, "p2/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "30", "failure", "2026-10-10T06:56:00Z", runImages("p1", "p1", "p1", "mixed"))
	seedRun(t, srv, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "31", "failure", "2026-10-10T07:49:00Z", runImages("p2", "p2", "p2", "p2"))
	// No candidate's images to match.
	seedRun(t, srv, "azure-ocp422-e2e-install-azure-blob-nightly", "32", "success", "2026-10-10T07:27:00Z", olmImages("p2", "p2", "p2", "p2", "p2"))
	seedFBC(t, srv, "quay-operator-fbc-3.18.2-20261010070000.ocp4.14", "2026-10-10T07:30:00", testNVR("quay-operator-metadata", "3.18.2"))
	get := func() model.ReleaseCandidate {
		t.Helper()
		var got model.ReleaseCandidate
		getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
		if got.Candidate != nil || got.Reason != "no release candidate yet" || got.CI == nil {
			t.Fatalf("3.18.2 candidate = %+v, reason %q, ci %+v; want none, no release candidate yet, CI", got.Candidate, got.Reason, got.CI)
		}
		return got
	}

	got := get()
	want := []string{
		"build quay-3-18-20261010-070000-000 2026-10-10T07:00:00Z none: gcp-ocp422-e2e-install-gcp-gcs-nightly=31",
		"build quay-3-18-20261010-045000-000 2026-10-10T05:00:00Z none: aws-ocp422-e2e-install-aws-s3-nightly=30",
		"unknown: azure-ocp422-e2e-install-azure-blob-nightly=32",
	}
	if rows := ciRows(got.CI.Builds); !slices.Equal(rows, want) {
		t.Fatalf("builds:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	if got.CI.Passed != nil || got.CI.FBC != nil || got.CI.Refresh != "none" || got.Newest == nil || got.Newest.Snapshot != "quay-3-18-20261010-070000-000" {
		t.Errorf("no run passed: passed %+v, fbc %+v, refresh %q, newest %+v; want none, none, none, quay-3-18-20261010-070000-000",
			got.CI.Passed, got.CI.FBC, got.CI.Refresh, got.Newest)
	}

	// A run of the first build passes.
	seedRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "33", "success", "2026-10-10T08:00:00Z", runImages("p1", "p1", "p1", "mixed"))
	if got := get(); got.CI.Passed == nil || got.CI.Passed.Snapshot != "quay-3-18-20261010-045000-000" || got.CI.Refresh != "pick" ||
		got.Newest == nil || got.Newest.Snapshot != "quay-3-18-20261010-070000-000" {
		t.Errorf("the first build passed: passed %+v, refresh %q, newest %+v; want quay-3-18-20261010-045000-000, pick, quay-3-18-20261010-070000-000",
			got.CI.Passed, got.CI.Refresh, got.Newest)
	}

	// A run of the newest build passes: no newest build, but the passed one
	// has its images.
	seedRun(t, srv, "gcp-ocp422-e2e-install-gcp-gcs-nightly", "34", "success", "2026-10-10T08:49:00Z", runImages("p2", "p2", "p2", "p2"))
	got = get()
	if got.CI.Passed == nil || got.CI.Passed.Snapshot != "quay-3-18-20261010-070000-000" || got.CI.Refresh != "pick" || got.Newest != nil {
		t.Fatalf("the newest build passed: passed %+v, refresh %q, newest %+v; want quay-3-18-20261010-070000-000, pick, none", got.CI.Passed, got.CI.Refresh, got.Newest)
	}
	quay := testNVR("quay-quay", "3.18.2")
	if cs := got.CI.Passed.Components; len(cs) != len(quay318) || cs[6] != (model.CandidateComponent{Name: "quay-3-18-quay-clair", Image: testImage("p2/quay-3-18-quay-clair")}) ||
		cs[9] != (model.CandidateComponent{Name: "quay-3-18-quay-quay", Image: testImage("p2/quay-3-18-quay-quay"), NVR: quay, BuildURL: "https://art.example/build?nvr=" + quay + "&record_id="}) {
		t.Errorf("passed components = %+v, want p2's images, its quay's NVR", cs)
	}
}

// The FBCs of the build CI passed with, to re-run release-from-fbc with: of
// the version's FBC images of each operator and OCP version, the one ART
// built last that holds a bundle of the build, by operator, then OCP version,
// and the pairs without one; none while one of them is unread.
func TestGetCandidateFBC(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	bundle := func(operator, built string) string {
		return operator + "-metadata-container-3.18.1." + built + ".p2.g35cf767.assembly.stream.el9-1"
	}
	quay, cso := bundle("quay-operator", "202610090339"), bundle("container-security-operator", "202610090339")
	resolveNVR(t, srv, "tested/quay-3-18-quay-operator-bundle", quay)
	resolveNVR(t, srv, "tested/quay-3-18-container-security-operator-bundle", cso)
	resolveNVR(t, srv, "tested/quay-3-18-quay-bridge-operator-bundle", bundle("quay-bridge-operator", "202610060910"))
	get := func() *model.BuildFBC {
		t.Helper()
		var got model.ReleaseCandidate
		getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
		if got.CI == nil || got.CI.Passed == nil || got.CI.Passed.Snapshot != "quay-3-18-20261009-042610-000" {
			t.Fatalf("3.18.1 ci = %+v, want the tested build passed", got.CI)
		}
		return got.CI.FBC
	}

	// Two FBCs of the operator for OCP 4.12 hold its bundle, and ART built
	// the one of the older NVR last. 3.18.2's FBC adds no pair.
	seedFBC(t, srv, "quay-operator-fbc-3.18.1-20261009050000.ocp4.12", "2026-10-09T05:30:00", quay)
	want := []string{
		seedFBC(t, srv, "container-security-operator-fbc-3.18.1-20261009044154.ocp5.0", "2026-10-09T05:42:26", cso),
		seedFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.9", "2026-10-09T06:14:00", "quay-operator-metadata-container-3.18.1.202610060910.p2.g35cf767.assembly.stream.el9-1", quay),
		seedFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.12", "2026-10-09T06:14:00", quay),
		seedFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp5.0", "2026-10-09T06:14:00", quay),
	}
	seedFBC(t, srv, "quay-operator-fbc-3.18.2-20261010120000.ocp4.14", "2026-10-10T12:30:00", testNVR("quay-operator-metadata", "3.18.2"))
	if got := get(); got == nil || !slices.Equal(got.Pullspecs, want) || len(got.Missing) != 0 {
		t.Errorf("fbc = %+v, want pullspecs %v, none missing", got, want)
	}

	// The bridge operator's FBC for OCP 4.12 holds a newer bundle than the build.
	seedFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261009044154.ocp4.12", "2026-10-09T05:42:26", bundle("quay-bridge-operator", "202610090339"))
	missing := []model.MissingFBC{{
		Operator: "quay-bridge-operator", OCP: "4.12",
		URL: "https://art.example/search?group=quay-3.18&name=quay-bridge-operator-fbc&assembly=stream",
	}}
	if got := get(); got == nil || !slices.Equal(got.Pullspecs, want) || !slices.Equal(got.Missing, missing) {
		t.Errorf("fbc = %+v, want pullspecs %v, missing %+v", got, want, missing)
	}

	// ART's record of a new FBC image of 3.18.1 is not read yet.
	resolveNVR(t, srv, "fbc/quay-operator-fbc-3.18.1-20261010013000.ocp4.12", "quay-operator-fbc-3.18.1-20261010013000.ocp4.12")
	if got := get(); got != nil {
		t.Errorf("fbc = %+v, want none until each FBC record is read", got)
	}
}

// The version's FBC catalogs: those ART staged, each with its Snapshot's
// newest Release, then the newest the FBC stream built of each operator and
// OCP version, unless ART staged it, by the oldest stream build holding it.
func TestGetCandidateCatalogs(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	ctx := t.Context()
	const app = "fbc-quay-3-18"
	op, cso, qbo := app+"-quay-operator", app+"-container-security-operator", app+"-quay-bridge-operator"
	snapshot := func(name, created string, images map[string]string) {
		t.Helper()
		id, err := srv.db.CreateSnapshot(ctx, app, name, mustTime(t, created))
		if err != nil {
			t.Fatal(err)
		}
		for c, img := range images {
			if err := srv.db.CreateSnapshotComponent(ctx, id, c, img); err != nil {
				t.Fatal(err)
			}
		}
	}
	release := func(name, snapshot, reason, created string) {
		t.Helper()
		r := &model.KonfluxRelease{Name: name, Application: app, Snapshot: snapshot, ReleasePlan: "quay-fbc-stage-3-18",
			ReleasedStatus: "False", ReleasedReason: reason, CreatedAt: mustTime(t, created)}
		if reason == "Succeeded" {
			r.ReleasedStatus = "True"
		}
		if err := srv.db.UpsertKonfluxRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	nvr := func(operator, version, built, ocp string) string {
		return operator + "-fbc-" + version + "-" + built + ".ocp" + ocp
	}

	// Staged only: a catalog ART has not identified, staged again after a
	// failed Release. Built and staged: the operator's catalog for OCP 4.17.
	const staged1, staged2 = "quay-stage-3-18-1-fbc-20261008084003", "quay-stage-3-18-1-fbc-20261008084006"
	snapshot(staged1, "2026-10-08T08:40:03Z", map[string]string{op: testImage("fbc/staged1")})
	snapshot(staged2, "2026-10-08T08:40:06Z", map[string]string{op: testImage("fbc/op-4.17")})
	for _, name := range []string{staged1, staged2} {
		if err := srv.db.UpsertStagedSnapshot(ctx, name, "3.18.1", "fbc", "stage"); err != nil {
			t.Fatal(err)
		}
	}
	release(staged1+"-a", staged1, "Failed", "2026-10-08T08:40:03Z")
	release(staged1+"-b", staged1, "Succeeded", "2026-10-08T09:00:00Z")

	// Built only: the newest CSO catalog for OCP 4.16 and operator catalog for
	// OCP 4.16. Older catalogs of a pair and 3.18.2's are left out.
	snapshot("fbc-quay-3-18-20261010-000000-000", "2026-10-10T00:00:00Z", map[string]string{
		op: testImage("fbc/op-4.17"), cso: testImage("fbc/cso-new"), qbo: testImage("fbc/qbo-3.18.2"),
	})
	snapshot("fbc-quay-3-18-20261009-000000-000", "2026-10-09T00:00:00Z", map[string]string{
		op: testImage("fbc/op-4.16"), cso: testImage("fbc/cso-new"),
	})
	snapshot("fbc-quay-3-18-20261008-000000-000", "2026-10-08T00:00:00Z", map[string]string{
		op: testImage("fbc/op-4.17-old"), cso: testImage("fbc/cso-old"),
	})
	for label, n := range map[string]string{
		"fbc/op-4.17":     nvr("quay-operator", "3.18.1", "20261008000000", "4.17"),
		"fbc/op-4.17-old": nvr("quay-operator", "3.18.1", "20261007000000", "4.17"),
		"fbc/op-4.16":     nvr("quay-operator", "3.18.1", "20261008000000", "4.16"),
		"fbc/cso-new":     nvr("container-security-operator", "3.18.1", "20261009000000", "4.16"),
		"fbc/cso-old":     nvr("container-security-operator", "3.18.1", "20261007000000", "4.16"),
		"fbc/qbo-3.18.2":  nvr("quay-bridge-operator", "3.18.2", "20261009000000", "4.16"),
	} {
		resolveNVR(t, srv, label, n)
	}

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	rows := []string{}
	for _, c := range got.Catalogs {
		row := strings.Join([]string{c.State, c.Snapshot, c.Component, c.Pullspec, c.NVR, c.OCP, c.ReleasePlan}, " ")
		if r := c.Release; r != nil {
			row += " " + r.Name + " " + r.ReleasedStatus
		}
		rows = append(rows, row)
	}
	want := []string{
		"staged " + staged1 + " " + op + " " + testImage("fbc/staged1") + "   quay-fbc-stage-3-18 " + staged1 + "-b True",
		"staged " + staged2 + " " + op + " " + testImage("fbc/op-4.17") + " " + nvr("quay-operator", "3.18.1", "20261008000000", "4.17") + " 4.17 ",
		"built fbc-quay-3-18-20261009-000000-000 " + cso + " " + testImage("fbc/cso-new") + " " + nvr("container-security-operator", "3.18.1", "20261009000000", "4.16") + " 4.16 ",
		"built fbc-quay-3-18-20261009-000000-000 " + op + " " + testImage("fbc/op-4.16") + " " + nvr("quay-operator", "3.18.1", "20261008000000", "4.16") + " 4.16 ",
	}
	if !slices.Equal(rows, want) {
		t.Errorf("catalogs:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

// The newest build shows beside the candidate when it holds another image of
// a component the candidate holds: like quay-3-17 for 3.17.6, the stream
// builds the CSO and QBO operators, which 3.18.2's STAGE build leaves out.
func TestGetCandidateNewest(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	operators := []string{"quay-3-18-container-security-operator", "quay-3-18-quay-bridge-operator"}
	stage := testBuild("stage2", slices.DeleteFunc(slices.Clone(quay318), func(c string) bool { return slices.Contains(operators, c) })...)
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", stage)
	if err := srv.db.UpsertStagedSnapshot(t.Context(), "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage"); err != nil {
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

	// A newer one with another quay. It holds the candidate's bundle, which
	// the candidate's own stage Release, not a per-build one, holds: it is not
	// in CI.
	next := maps.Clone(same)
	next["quay-3-18-quay-quay"] = testImage("next/quay-3-18-quay-quay")
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	seedImages(t, srv, "quay-3-18-20261010-070000-000", "2026-10-10T07:00:00Z", next)
	got = model.ReleaseCandidate{}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if n := got.Newest; n == nil || n.Snapshot != "quay-3-18-20261010-070000-000" || n.CI != (model.CIState{State: "none"}) {
		t.Errorf("3.18.2 newest = %+v, want quay-3-18-20261010-070000-000, not in CI", n)
	}
}

// A component the STAGE build's shipment leaves out shows as built, by its
// newest image of the version: an older build of the version and a stream
// build of another version do not count.
func TestGetCandidateUnshipped(t *testing.T) {
	srv := setupTestServer(t)
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	ctx := t.Context()
	if err := srv.db.UpsertReleaseVersion(ctx, &model.ReleaseVersion{Name: "quay-v3.18.2", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	const (
		operator = "quay-3-18-quay-operator"
		cso      = "quay-3-18-container-security-operator"
		qbo      = "quay-3-18-quay-bridge-operator"
		stage    = "quay-stage-3-18-2-image-20261010040000"
	)
	seedImages(t, srv, stage, "2026-10-10T04:00:00Z", testBuild("stage", operator))
	if err := srv.db.UpsertStagedSnapshot(ctx, stage, "3.18.2", "image", "stage"); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, stage, stage, "quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	seedImages(t, srv, "quay-3-18-20261010-050000-000", "2026-10-10T05:00:00Z", testBuild("old", cso))
	resolveNVR(t, srv, "old/"+cso, testNVR("container-security-operator", "3.18.2"))
	seedImages(t, srv, "quay-3-18-20261010-060000-000", "2026-10-10T06:00:00Z", testBuild("new", operator, cso))
	resolveNVR(t, srv, "new/"+operator, testNVR("quay-operator", "3.18.2"))
	resolveNVR(t, srv, "new/"+cso, testNVR("container-security-operator", "3.18.2"))
	seedImages(t, srv, "quay-3-18-20261011-060000-000", "2026-10-11T06:00:00Z", testBuild("next", cso, qbo))
	resolveNVR(t, srv, "next/"+cso, testNVR("container-security-operator", "3.18.3"))
	resolveNVR(t, srv, "next/"+qbo, testNVR("quay-bridge-operator", "3.18.3"))

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	nvr := testNVR("container-security-operator", "3.18.2")
	want := []model.UnshippedBuild{{
		CandidateComponent: model.CandidateComponent{Name: cso, Image: testImage("new/" + cso), NVR: nvr, BuildURL: "https://art.example/build?nvr=" + nvr + "&record_id="},
		Snapshot:           "quay-3-18-20261010-060000-000",
	}}
	if !reflect.DeepEqual(got.UnshippedBuilds, want) {
		t.Errorf("3.18.2 unshipped builds = %+v, want %+v", got.UnshippedBuilds, want)
	}
}

// A build of a version counts for it only if ART built no lower version
// after it: ART built 3.18.2 until 28 Sep, then went back to 3.18.1. Its next
// 3.18.2 build counts, and hides no older 3.18.1 build.
func TestGetCandidateVersionSwitch(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	ctx := t.Context()
	// 3.18.2's STAGE build gives it a candidate, though ART builds 3.18.1, and
	// its newest build shows beside it.
	seedImages(t, srv, "quay-stage-3-18-2-image-20261010040000", "2026-10-10T04:00:00Z", testBuild("stage2", quay318...))
	if err := srv.db.UpsertStagedSnapshot(ctx, "quay-stage-3-18-2-image-20261010040000", "3.18.2", "image", "stage"); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "quay-stage-3-18-2-image-20261010040000", "quay-stage-3-18-2-image-20261010040000",
		"quay-advisory-stage-3-18", "Succeeded", "2026-10-10T04:00:00Z", "2026-10-10T04:10:00Z")
	seedImages(t, srv, "quay-3-18-20260928-214539-000", "2026-09-28T21:57:16Z", testBuild("early", quay318...))
	resolveNVR(t, srv, "early/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if c := got.Candidate; c == nil || c.Snapshot != "quay-stage-3-18-2-image-20261010040000" || got.Reason != "" {
		t.Fatalf("3.18.2 candidate = %+v, reason %q; want its STAGE build, no reason", c, got.Reason)
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
// at all has no newest build either, and says when ART still builds a lower
// version.
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
		{Name: "quay-v3.16.7", KonfluxApplication: "quay-3-16"},
		{Name: "3.16.3"},
	} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &v); err != nil {
			t.Fatal(err)
		}
	}
	seedSnapshot(t, srv, "quay-3-20", "quay-3-20-20261010-000000-000", mustTime(t, "2026-10-10T00:00:00Z"), "quay-3-20-quay-quay")
	seedSnapshot(t, srv, "quay-3-16", "quay-3-16-20261010-000000-000", mustTime(t, "2026-10-10T00:00:00Z"), "quay-3-16-quay-quay")
	resolveNVR(t, srv, "quay-3-16-20261010-000000-000/quay-3-16-quay-quay", testNVR("quay-quay", "3.16.8"))
	none := func(shipped bool, reason string) map[string]any {
		return map[string]any{"shipped": shipped, "candidate": nil, "reason": reason, "newest": nil, "ci": nil, "catalogs": nil, "unshipped_builds": nil, "fbc_candidate": nil, "fbc_stream": nil}
	}
	for version, want := range map[string]map[string]any{
		"quay-v3.18.0": none(true, ""), // released in JIRA
		"quay-v3.18.3": none(true, ""), // published in the catalog
		"omr-v2.0.13":  none(false, "not a concrete quay-vX.Y.Z version"),
		"3.16.3":       none(false, "no Konflux application"),
		// ART resolved no build of quay-3-20, and builds 3.16.8.
		"quay-v3.20.0": none(false, "no release candidate yet"),
		"quay-v3.16.7": none(false, "no release candidate yet"),
		// Every quay-3-18 build is of 3.18.1, or of no version.
		"quay-v3.18.2": none(false, "no 3.18.2 builds yet: ART still builds 3.18.1"),
	} {
		var got map[string]any
		getJSON(t, srv, "/api/v1/releases/"+version+"/candidate", http.StatusOK, &got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", version, got, want)
		}
	}
	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/candidate", http.StatusNotFound, nil)
}

// A shipped version, released in JIRA or published in the catalog, keeps its
// release candidate and the CI that tested it, with nothing to pick, re-run
// or wait on.
func TestGetCandidateShipped(t *testing.T) {
	for _, how := range []string{"jira", "catalog"} {
		t.Run(how, func(t *testing.T) {
			srv := setupTestServer(t)
			seedQuay318(t, srv)
			var before model.ReleaseCandidate
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &before)
			if before.CI == nil || before.CI.Refresh != "rerun" || before.Newest == nil {
				t.Fatalf("unshipped 3.18.1: ci %+v, newest %+v; want rerun and a newest build", before.CI, before.Newest)
			}
			if how == "jira" {
				if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{"total":1,"data":[{"repositories":[{"published":true,"tags":[{"name":"v3.18.1"}]}]}]}`))
				}))
				defer cat.Close()
				srv.shipped = catalog.NewShipped(catalog.NewClient(cat.URL, cat.Client()), slog.Default())
				srv.shipped.Refresh(t.Context())
			}

			var got model.ReleaseCandidate
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
			if c := got.Candidate; !got.Shipped || got.Reason != "" || got.Newest != nil || c == nil ||
				c.Snapshot != before.Candidate.Snapshot || len(c.Components) != len(before.Candidate.Components) || c.Prod == nil {
				t.Fatalf("shipped 3.18.1: shipped %v, reason %q, newest %+v, candidate %+v", got.Shipped, got.Reason, got.Newest, c)
			}
			ci := got.CI
			if ci == nil || !slices.Equal(ciRows(ci.Builds), ciRows(before.CI.Builds)) {
				t.Fatalf("shipped 3.18.1 ci = %+v, want the unshipped rows %v", ci, ciRows(before.CI.Builds))
			}
			if ci.LatestInCI != nil || ci.Passed != nil || ci.FBC != nil || ci.Refresh != "current" {
				t.Errorf("shipped 3.18.1: latest %+v, passed %+v, fbc %+v, refresh %q; want none, none, none, current", ci.LatestInCI, ci.Passed, ci.FBC, ci.Refresh)
			}
		})
	}
}

// seedHistoryFBC stores ART's successful build of the FBC image of nvr,
// rendered with its release channel headed by the bundle of NVR bundle. It
// returns the image's pullspec.
func seedHistoryFBC(t *testing.T, srv *Server, nvr, bundle string) string {
	t.Helper()
	_, version, _, _ := artbuild.ParseFBC(nvr)
	pullspec := testImage("fbc/" + nvr)
	if err := srv.db.UpsertArtHistoryBuild(t.Context(), artbuild.HistoryBuild{
		RecordID: "rec-" + nvr, Group: "quay-" + version[:strings.LastIndex(version, ".")], Type: "fbc",
		NVR: nvr, Digest: testDigest("fbc/" + nvr), Pullspec: pullspec,
	}); err != nil {
		t.Fatal(err)
	}
	seedRender(t, srv, pullspec, "", bundle)
	return pullspec
}

// seedRender stores the render of the FBC image pullspec, labelled label,
// whose channel stable is headed by the CSV csv-<bundle> of the bundle of NVR
// bundle, with that bundle's ART build.
func seedRender(t *testing.T, srv *Server, pullspec, label, bundle string) {
	t.Helper()
	ctx := t.Context()
	img := testImage("bundle/" + bundle)
	if err := srv.db.UpsertArtHistoryBuild(ctx, artbuild.HistoryBuild{
		RecordID: "rec-" + bundle, Group: "quay", Type: "bundle", NVR: bundle, Digest: testDigest("bundle/" + bundle), Pullspec: img,
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.UpsertFBCRender(ctx, fbc.Render{
		Digest: imageDigest(pullspec), State: fbc.StateRendered, LabelNVR: label,
		Channel: fbc.Channel{Name: "stable", HeadCSV: "csv-" + bundle, HeadBundlePullspec: img}, RenderedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// seedStagedFBC stores app's FBC Snapshot name, holding images, which ART
// staged for stage for assembly, and its Releases, oldest first, each ending
// with a reason and created a minute apart.
func seedStagedFBC(t *testing.T, srv *Server, app, assembly, name, created string, images []string, reasons ...string) {
	t.Helper()
	ctx := t.Context()
	id, err := srv.db.CreateSnapshot(ctx, app, name, mustTime(t, created))
	if err != nil {
		t.Fatal(err)
	}
	for i, img := range images {
		if err := srv.db.CreateSnapshotComponent(ctx, id, fmt.Sprintf("catalog-%d", i), img); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.db.UpsertStagedSnapshot(ctx, name, assembly, "fbc", "stage"); err != nil {
		t.Fatal(err)
	}
	for i, reason := range reasons {
		r := &model.KonfluxRelease{
			Name: fmt.Sprintf("%s-%d", name, i), Application: app, Snapshot: name, ReleasePlan: "quay-fbc-stage",
			ReleasedStatus: "False", ReleasedReason: reason, CreatedAt: mustTime(t, created).Add(time.Duration(i+1) * time.Minute),
		}
		if reason == "Succeeded" {
			r.ReleasedStatus = "True"
		}
		if err := srv.db.UpsertKonfluxRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
}

// getFBC returns the FBC candidate and stream of version, of application app.
func getFBC(t *testing.T, srv *Server, version, app string) (cand, stream *model.FBCCandidate) {
	t.Helper()
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: version, KonfluxApplication: app}); err != nil {
		t.Fatal(err)
	}
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/"+version+"/candidate", http.StatusOK, &got)
	if got.FBCCandidate == nil || got.FBCStream == nil {
		t.Fatalf("fbc_candidate %v, fbc_stream %v, want both", got.FBCCandidate, got.FBCStream)
	}
	return got.FBCCandidate, got.FBCStream
}

// fbcRows is c's source, state and errors, then each operator's head bundle,
// coverage, members, conflicts and missing OCP versions, then its unknown
// catalogs.
func fbcRows(c *model.FBCCandidate) []string {
	member := func(m model.FBCMember) string {
		s := m.OCP + ":" + m.NVR
		if m.Snapshot != "" {
			s += "@" + m.Snapshot
		}
		if m.Release != nil {
			s += "/" + m.Release.ReleasedReason
		}
		return s
	}
	rows := []string{c.Source + " " + c.State + " [" + strings.Join(c.Errors, "; ") + "]"}
	for _, o := range c.Operators {
		row := fmt.Sprintf("%s %q %d/%d", o.Operator, o.HeadBundleNVR, o.Coverage.Members, o.Coverage.Slots)
		for _, m := range o.Members {
			row += " " + member(m)
		}
		for _, m := range o.Conflicts {
			row += " conflict=" + member(m)
		}
		if len(o.MissingOCP) > 0 {
			row += " missing=" + strings.Join(o.MissingOCP, ",")
		}
		rows = append(rows, row)
	}
	for _, m := range c.Unknown {
		rows = append(rows, "unknown "+m.Pullspec+" "+member(m))
	}
	return rows
}

func checkRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n%s\nwant:\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Like 3.16 on 12 Oct 2026: ART builds the stream, keyed by the placeholder
// head v3.99.0-dev, and staged nothing, so the candidate is the stream.
func TestGetCandidateFBCStream(t *testing.T) {
	srv := setupTestServer(t)
	const head = "quay-operator-metadata-container-3.16.7.202610121915"
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012201007.ocp4.12", head)
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261012200000.ocp4.13", head)
	// Superseded in its slot, and another z of the stream.
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.7-20261011000000.ocp4.12", "quay-operator-metadata-container-3.16.7.202610110000")
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.16.6-20261012201007.ocp4.14", "quay-operator-metadata-container-3.16.6.202610121915")
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.16.7-20261012201007.ocp4.12", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12", "qbo-bundle")

	cand, stream := getFBC(t, srv, "quay-v3.16.7", "quay-3-16")
	want := []string{
		"stream complete []",
		`quay-operator "` + head + `" 2/2 4.12:quay-operator-fbc-3.16.7-20261012201007.ocp4.12 4.13:quay-operator-fbc-3.16.7-20261012200000.ocp4.13`,
		`container-security-operator "cso-bundle" 1/1 4.12:container-security-operator-fbc-3.16.7-20261012201007.ocp4.12`,
		`quay-bridge-operator "qbo-bundle" 1/1 4.12:quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12`,
	}
	checkRows(t, "candidate", fbcRows(cand), want)
	checkRows(t, "stream", fbcRows(stream), want)
	if o := cand.Operators[0]; o.Channel != "stable" || o.HeadCSV != "csv-"+head || o.Members[0].BuildURL == "" {
		t.Errorf("quay-operator channel %q head %q build %q", o.Channel, o.HeadCSV, o.Members[0].BuildURL)
	}
}

// Like 3.17.6: ART staged quay-operator's 9 FBCs, all on bundle ...0936, and
// no CSO or QBO FBC, which the stream builds: those are errors.
func TestGetCandidateFBCMissingOperator(t *testing.T) {
	srv := setupTestServer(t)
	const head = "quay-operator-metadata-container-3.17.6.202610020936"
	var staged []string
	want := `quay-operator "` + head + `" 9/9`
	for minor := 12; minor <= 20; minor++ {
		nvr := fmt.Sprintf("quay-operator-fbc-3.17.6-20261002100000.ocp4.%d", minor)
		staged = append(staged, seedHistoryFBC(t, srv, nvr, head))
		want += fmt.Sprintf(" 4.%d:%s@quay-stage-3-17-6-fbc-20261002120000/Succeeded", minor, nvr)
	}
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.17.6-20261002100000.ocp4.12", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.17.6-20261002100000.ocp4.12", "qbo-bundle")
	seedStagedFBC(t, srv, "fbc-quay-3-17", "3.17.6", "quay-stage-3-17-6-fbc-20261002120000", "2026-10-02T12:00:00Z", staged, "Succeeded")

	cand, stream := getFBC(t, srv, "quay-v3.17.6", "quay-3-17")
	checkRows(t, "candidate", fbcRows(cand), []string{
		"stage_snapshots incomplete [container-security-operator is not in stage; quay-bridge-operator is not in stage]",
		want,
		`container-security-operator "" 0/1 missing=4.12`,
		`quay-bridge-operator "" 0/1 missing=4.12`,
	})
	if stream.Source != "stream" || stream.Operators[1].Coverage.Members != 1 {
		t.Errorf("stream %s, CSO coverage %+v, want the stream's CSO FBC", stream.Source, stream.Operators[1].Coverage)
	}
}

// Like 3.18.1: ART restaged slots. Each slot takes the newest staged Snapshot
// holding its catalog, with that Snapshot's newest Release even when it
// failed, never the union of every staged catalog. A catalog no build history
// names is identified by its resolved ART build, else its label; one nothing
// names is unknown and fills no slot.
func TestGetCandidateFBCRestage(t *testing.T) {
	srv := setupTestServer(t)
	const app, head = "fbc-quay-3-18", "quay-operator-metadata-container-3.18.1.202610090339"
	const old, restaged = "quay-stage-3-18-1-fbc-20261008084003", "quay-stage-3-18-1-fbc-20261009050000"
	op20old := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261008000000.ocp4.20", head)
	op20 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.20", head)
	op21 := testImage("fbc/labelled")
	seedRender(t, srv, op21, "quay-operator-fbc-3.18.1-20261008000000.ocp4.21", head)
	cso := testImage("fbc/cso")
	resolveNVR(t, srv, "fbc/cso", "container-security-operator-fbc-3.18.1-20261008000000.ocp4.20")
	seedRender(t, srv, cso, "", "cso-bundle")
	qbo := seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261008000000.ocp4.20", "qbo-bundle")
	unnamed := testImage("fbc/unnamed")
	seedStagedFBC(t, srv, app, "3.18.1", old, "2026-10-08T08:40:03Z", []string{op20old, op21, cso, qbo, unnamed}, "Succeeded")
	seedStagedFBC(t, srv, app, "3.18.1", restaged, "2026-10-09T05:00:00Z", []string{op20, unnamed}, "Succeeded", "Failed")

	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	checkRows(t, "candidate", fbcRows(cand), []string{
		"stage_snapshots unknown []",
		`quay-operator "` + head + `" 2/2 4.20:quay-operator-fbc-3.18.1-20261009044841.ocp4.20@` + restaged + `/Failed 4.21:quay-operator-fbc-3.18.1-20261008000000.ocp4.21@` + old + `/Succeeded`,
		`container-security-operator "cso-bundle" 1/1 4.20:container-security-operator-fbc-3.18.1-20261008000000.ocp4.20@` + old + `/Succeeded`,
		`quay-bridge-operator "qbo-bundle" 1/1 4.20:quay-bridge-operator-fbc-3.18.1-20261008000000.ocp4.20@` + old + `/Succeeded`,
		"unknown " + unnamed + " :@" + restaged + "/Failed",
	})
}

// An operator whose slots head different bundles is keyed by its newest
// FBC's: the others are conflicts, not members, and the candidate is
// inconsistent.
func TestGetCandidateFBCMixed(t *testing.T) {
	srv := setupTestServer(t)
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.17.6-20261002100000.ocp4.12", "qo-bundle")
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.17.6-20261003100000.ocp4.12", "cso-1613")
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.17.6-20261002100000.ocp4.13", "cso-0313")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.17.6-20261002100000.ocp4.12", "qbo-bundle")

	cand, _ := getFBC(t, srv, "quay-v3.17.6", "quay-3-17")
	checkRows(t, "candidate", fbcRows(cand), []string{
		"stream inconsistent [container-security-operator heads more than one bundle across its OCP versions]",
		`quay-operator "qo-bundle" 1/1 4.12:quay-operator-fbc-3.17.6-20261002100000.ocp4.12`,
		`container-security-operator "cso-1613" 1/2 4.12:container-security-operator-fbc-3.17.6-20261003100000.ocp4.12 conflict=4.13:container-security-operator-fbc-3.17.6-20261002100000.ocp4.13`,
		`quay-bridge-operator "qbo-bundle" 1/1 4.12:quay-bridge-operator-fbc-3.17.6-20261002100000.ocp4.12`,
	})
}
