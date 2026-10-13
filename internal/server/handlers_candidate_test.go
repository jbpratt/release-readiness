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

// A build's version is the X.Y.Z the NVRs of its operator bundles ART
// resolved carry, else of its images, base image left out.
func TestBuildVersion(t *testing.T) {
	const app = "quay-3-18"
	image := func(c string) model.SnapshotImage { return model.SnapshotImage{Name: c, Image: testImage("b/" + c)} }
	snap := &model.ReleaseSnapshot{Components: []model.SnapshotImage{
		image("quay-3-18-base-rhel9"), image("quay-3-18-container-security-operator-bundle"), image("quay-3-18-quay-clair"),
		image("quay-3-18-quay-operator-bundle"), image("quay-3-18-quay-quay"),
	}}
	const (
		quay       = "quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9"
		bundle     = "quay-operator-metadata-container-3.18.1.202610061637.p2.g35cf767.assembly.stream.el9-1"
		bundle2    = "quay-operator-metadata-container-3.18.2.202610122226.p2.g35cf767.assembly.stream.el9-1"
		csoBundle2 = "container-security-operator-metadata-container-3.18.2.202610122226.p2.g1a2b3c4.assembly.stream.el9-1"
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
		{"bundles over images", map[string]string{
			"quay-3-18-quay-quay": quay, "quay-3-18-quay-operator-bundle": bundle2, "quay-3-18-container-security-operator-bundle": csoBundle2,
		}, "3.18.2"},
		{"no bundle resolved", map[string]string{"quay-3-18-quay-quay": quay, "quay-3-18-quay-clair": testNVR("quay-clair", "3.18.1")}, "3.18.1"},
		{"two bundle versions", map[string]string{"quay-3-18-quay-operator-bundle": bundle, "quay-3-18-container-security-operator-bundle": csoBundle2}, ""},
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

	// 3.18.1's STAGE build carries the prod Release: the newest Release of a
	// prod Snapshot, not the newest prod Snapshot. Its Snapshot and images are
	// only checked against, so not served.
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	c := got.Candidate
	if got.Shipped || got.Reason != "" || c == nil {
		t.Fatalf("3.18.1 candidate = %+v, reason %q", c, got.Reason)
	}
	if p := c.Prod; p == nil || p.Name != "quay-prod-3-18-1-image-20261009133455" || p.ReleasedReason != "Failed" || p.FailedTask != "verify-conforma" {
		t.Errorf("3.18.1 prod = %+v", p)
	}
	var raw map[string]any
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &raw)
	if keys := slices.Sorted(maps.Keys(raw["candidate"].(map[string]any))); !slices.Equal(keys, []string{"prod"}) {
		t.Errorf("3.18.1 candidate keys = %v, want prod only", keys)
	}

	// 3.14.10 has no STAGE build, so no release candidate, though it has a
	// build.
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.14.10", KonfluxApplication: "quay-3-14"}); err != nil {
		t.Fatal(err)
	}
	seedSnapshot(t, srv, "quay-3-14", "quay-3-14-20261009-185801-000", mustTime(t, "2026-10-09T19:27:56Z"),
		"quay-3-14-base-rhel8", "quay-3-14-quay-builder", "quay-3-14-quay-builder-qemu", "quay-3-14-quay-clair")
	resolveNVR(t, srv, "quay-3-14-20261009-185801-000/quay-3-14-quay-clair", testNVR("quay-clair", "3.14.10"))
	scanHistory(t, srv, "quay-3.14")
	raw = nil
	getJSON(t, srv, "/api/v1/releases/quay-v3.14.10/candidate", http.StatusOK, &raw)
	want := map[string]any{
		"shipped": false, "candidate": nil, "reason": "no release candidate yet", "catalogs": nil, "unshipped_builds": nil,
		"fbc_candidate": nil, "fbc_stream": nil, "ci_history": nil,
		"verdict": map[string]any{"state": "not_ready", "items": []any{}},
		"reading": map[string]any{"read": 0.0, "failed": 0.0, "pending": 0.0, "history_days": 0.0},
	}
	if !reflect.DeepEqual(raw, want) {
		t.Errorf("3.14.10 = %v, want %v", raw, want)
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
		r := &model.KonfluxRelease{
			Name: name, Application: app, Snapshot: snapshot, ReleasePlan: "quay-fbc-stage-3-18",
			ReleasedStatus: "False", ReleasedReason: reason, CreatedAt: mustTime(t, created),
		}
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
	newest := func(version string) string {
		t.Helper()
		c, err := srv.selectCandidate(ctx, &model.ReleaseVersion{Name: version, KonfluxApplication: "quay-3-18"})
		if err != nil {
			t.Fatal(err)
		}
		if c.newest == nil {
			return ""
		}
		return c.newest.Name
	}
	seedImages(t, srv, "quay-3-18-20260928-214539-000", "2026-09-28T21:57:16Z", testBuild("early", quay318...))
	resolveNVR(t, srv, "early/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	if n := newest("quay-v3.18.2"); n != "" {
		t.Errorf("3.18.2 newest = %q, want no 28 Sep build", n)
	}

	seedImages(t, srv, "quay-3-18-20261011-000000-000", "2026-10-11T00:00:00Z", testBuild("next", quay318...))
	resolveNVR(t, srv, "next/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.2"))
	if n := newest("quay-v3.18.2"); n != "quay-3-18-20261011-000000-000" {
		t.Errorf("3.18.2 newest = %q, want quay-3-18-20261011-000000-000", n)
	}
	if n := newest("quay-v3.18.1"); n != "quay-3-18-20261010-013713-000" {
		t.Errorf("3.18.1 newest = %q, want quay-3-18-20261010-013713-000", n)
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
	scanHistory(t, srv, "quay-3.16", "quay-3.18", "quay-3.20")
	none := func(shipped bool, reason string) map[string]any {
		state := "not_ready"
		if shipped {
			state = "shipped"
		}
		return map[string]any{
			"shipped": shipped, "candidate": nil, "reason": reason, "catalogs": nil, "unshipped_builds": nil, "fbc_candidate": nil, "fbc_stream": nil, "ci_history": nil,
			"verdict": map[string]any{"state": state, "items": []any{}},
			"reading": map[string]any{"read": 0.0, "failed": 0.0, "pending": 0.0, "history_days": 0.0},
		}
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

// Like 3.18.2 on 12 Oct 2026: ART moved the stream to 3.18.2 and rebuilt its
// bundles but not quay or the builder, still 3.18.1. The build is 3.18.2's,
// and its operator bundle is checked against the 3.18.2 FBC's head.
func TestGetCandidateBundleVersion(t *testing.T) {
	srv := setupTestServer(t)
	seedQuay318(t, srv)
	const (
		build = "quay-3-18-20261012-223000-000"
		head  = "quay-operator-metadata-container-3.18.2.202610122226.p2.g35cf767.assembly.stream.el9-1"
		fbc   = "quay-operator-fbc-3.18.2-20261012232700.ocp4.20"
	)
	seedImages(t, srv, build, "2026-10-12T22:30:00Z", testBuild("bumped", quay318...))
	resolveNVR(t, srv, "bumped/quay-3-18-quay-operator-bundle", head)
	resolveNVR(t, srv, "bumped/quay-3-18-container-security-operator-bundle", "container-security-operator-metadata-container-3.18.2.202610122226.p2.g1a2b3c4.assembly.stream.el9-1")
	resolveNVR(t, srv, "bumped/quay-3-18-quay-quay", testNVR("quay-quay", "3.18.1"))
	resolveNVR(t, srv, "bumped/quay-3-18-quay-builder", testNVR("quay-builder", "3.18.1"))
	seedHistoryFBC(t, srv, fbc, head)
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.18.2-20261012232700.ocp4.20", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.2-20261012232700.ocp4.20", "qbo-bundle")

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/candidate", http.StatusOK, &got)
	if got.Reason != "no release candidate yet" || got.FBCStream == nil {
		t.Fatalf("3.18.2: reason %q, fbc_stream %v", got.Reason, got.FBCStream)
	}
	checkRows(t, "stream", fbcRows(got.FBCStream), []string{
		"stream complete []",
		`quay-operator "` + head + `" 1/1 4.20:` + fbc,
		`container-security-operator "cso-bundle" 1/1 4.20:container-security-operator-fbc-3.18.2-20261012232700.ocp4.20`,
		`quay-bridge-operator "qbo-bundle" 1/1 4.20:quay-bridge-operator-fbc-3.18.2-20261012232700.ocp4.20`,
	})
	if c, want := got.FBCStream.Operators[0].ImageCheck, (model.ImageCheck{State: "matches", Snapshot: build, BundleNVR: head}); c != want {
		t.Errorf("image check %+v, want %+v", c, want)
	}
}

// A shipped version, released in JIRA or published in the catalog, keeps its
// STAGE build's prod Release, with no reason.
func TestGetCandidateShipped(t *testing.T) {
	for _, how := range []string{"jira", "catalog"} {
		t.Run(how, func(t *testing.T) {
			srv := setupTestServer(t)
			seedQuay318(t, srv)
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
			// Reading counts the shipped FBCs, not a newer one only the stream
			// built, which RR has yet to read.
			op := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261008000000.ocp4.20", "quay-operator-bundle")
			seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "quay-stage-3-18-1-fbc-20261008000000", "2026-10-08T00:00:00Z", []string{op}, "Succeeded")
			const newer = "quay-operator-fbc-3.18.1-20261010000000.ocp4.20"
			if err := srv.db.UpsertArtHistoryBuild(t.Context(), artbuild.HistoryBuild{
				RecordID: "rec-" + newer, Group: "quay-3.18", Type: "fbc", NVR: newer, Digest: testDigest("fbc/" + newer), Pullspec: testImage("fbc/" + newer),
			}); err != nil {
				t.Fatal(err)
			}

			var got model.ReleaseCandidate
			getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
			if c := got.Candidate; !got.Shipped || got.Reason != "" || c == nil || c.Prod == nil {
				t.Fatalf("shipped 3.18.1: shipped %v, reason %q, candidate %+v", got.Shipped, got.Reason, c)
			}
			if got.Reading != (model.ReadProgress{Read: 1}) {
				t.Errorf("shipped 3.18.1: reading %+v, want 1 read, none pending", got.Reading)
			}
		})
	}
}

// scanHistory marks every day the ART history sync reads of each group
// scanned.
func scanHistory(t *testing.T, srv *Server, groups ...string) {
	t.Helper()
	for i := range 120 {
		day := time.Now().UTC().AddDate(0, 0, -i).Format(time.DateOnly)
		for _, g := range groups {
			if err := srv.db.MarkArtHistoryDayScanned(t.Context(), g, artbuild.TypeFBC, day, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
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
	// Complete: release-from-fbc takes each slot's FBC, by operator, then OCP.
	if want := []string{
		testImage("fbc/quay-operator-fbc-3.16.7-20261012201007.ocp4.12"),
		testImage("fbc/quay-operator-fbc-3.16.7-20261012200000.ocp4.13"),
		testImage("fbc/container-security-operator-fbc-3.16.7-20261012201007.ocp4.12"),
		testImage("fbc/quay-bridge-operator-fbc-3.16.7-20261012201007.ocp4.12"),
	}; !slices.Equal(cand.Pullspecs, want) {
		t.Errorf("pullspecs %v, want %v", cand.Pullspecs, want)
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
	// A partial shipment cannot be released, though the stream could be.
	if len(cand.Pullspecs) != 0 || len(stream.Pullspecs) != 11 {
		t.Errorf("pullspecs: candidate %v, stream %d; want none, 11", cand.Pullspecs, len(stream.Pullspecs))
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

// A digest on stage stays on stage when a newer Snapshot restages it and that
// Release fails: its member shows the Release that succeeded, and nothing
// blocks on the failure.
func TestGetCandidateFBCRestageFailed(t *testing.T) {
	srv := setupTestServer(t)
	const app, head = "fbc-quay-3-18", "quay-operator-metadata-container-3.18.1.202610090339"
	const first, restaged = "quay-stage-3-18-1-fbc-20261008000000", "quay-stage-3-18-1-fbc-20261009000000"
	op20 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261008000000.ocp4.20", head)
	seedStagedFBC(t, srv, app, "3.18.1", first, "2026-10-08T00:00:00Z", []string{op20}, "Succeeded")
	seedStagedFBC(t, srv, app, "3.18.1", restaged, "2026-10-09T00:00:00Z", []string{op20}, "Failed")
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	o := got.FBCCandidate.Operators[0]
	if m := o.Members[0]; m.Snapshot != first || !succeeded(m.Release) || o.Stage != (model.StageSummary{Staged: 1, Total: 1}) {
		t.Errorf("quay-operator member %s %+v, stage %+v; want %s Succeeded, 1 of 1 on stage", m.Snapshot, m.Release, o.Stage, first)
	}
	for _, item := range got.Verdict.Items {
		if item.Code == "stage_release_failed" {
			t.Errorf("verdict item %+v: the digest is on stage", item)
		}
	}
}

// Like 3.18.1's restaged slots: a slot counts each staged Snapshot holding a
// catalog of it whose newest Release succeeded, never one that failed and was
// superseded; an operator counts its slots' FBCs on stage. A staged FBC RR
// failed to read blocks the release, as does one no stage Release of it
// succeeded, whatever RR read and scanned. Reading counts only the version's
// FBCs on the page, not a superseded one or another version's.
func TestGetCandidateFBCStaged(t *testing.T) {
	srv := setupTestServer(t)
	const app, head = "fbc-quay-3-18", "quay-operator-metadata-container-3.18.1.202610090339"
	op20old := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261007000000.ocp4.20", head)
	op20 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009000000.ocp4.20", head)
	op21 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261007000000.ocp4.21", head)
	qbo := seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261007000000.ocp4.20", "qbo-bundle")
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.2-20261009000000.ocp4.20", head)
	const csoNVR = "container-security-operator-fbc-3.18.1-20261007000000.ocp4.20"
	cso := testImage("fbc/" + csoNVR)
	if err := srv.db.UpsertArtHistoryBuild(t.Context(), artbuild.HistoryBuild{
		RecordID: "rec-cso", Group: "quay-3.18", Type: "fbc", NVR: csoNVR, Digest: imageDigest(cso), Pullspec: cso,
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.UpsertFBCRender(t.Context(), fbc.Render{
		Digest: imageDigest(cso), State: fbc.StateFailed, Error: "no channel stable-3.18", RenderedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	seedStagedFBC(t, srv, app, "3.18.1", "quay-stage-3-18-1-fbc-20261007000000", "2026-10-07T00:00:00Z", []string{op20old, op21, qbo}, "Failed")
	seedStagedFBC(t, srv, app, "3.18.1", "quay-stage-3-18-1-fbc-20261008000000", "2026-10-08T00:00:00Z", []string{op20old, qbo}, "Succeeded")
	seedStagedFBC(t, srv, app, "3.18.1", "quay-stage-3-18-1-fbc-20261009000000", "2026-10-09T00:00:00Z", []string{op20, cso}, "Succeeded")
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}

	want := model.Verdict{State: "not_ready", Items: []model.VerdictItem{{
		Level: "blocker", Operator: "quay-operator", Owner: "ART/Konflux", Code: "stage_release_failed",
		Text: "quay-operator OCP 4.21: stage release failed",
	}, {
		Level: "blocker", Operator: "container-security-operator", Owner: "Quay/ART", Code: "read_failed",
		Text: "container-security-operator OCP 4.20: can't read FBC: no channel stable-3.18",
	}}}
	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	if !reflect.DeepEqual(got.Verdict, want) || got.Reading.HistoryDays == 0 {
		t.Errorf("unscanned history: verdict %+v, reading %+v; want %+v, history days left", got.Verdict, got.Reading, want)
	}
	scanHistory(t, srv, "quay-3.18")
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	o := got.FBCCandidate.Operators[0]
	if len(o.Members) != 2 || o.Members[0].Staged != 2 || o.Members[1].Staged != 0 || o.Stage != (model.StageSummary{Staged: 1, Total: 2}) {
		t.Errorf("quay-operator members %+v, stage %+v; want 4.20 staged 2, 4.21 staged 0, 1 of 2 on stage", o.Members, o.Stage)
	}
	if !reflect.DeepEqual(got.Verdict, want) || got.Reading != (model.ReadProgress{Read: 3, Failed: 1}) {
		t.Errorf("verdict %+v, reading %+v; want %+v, 3 read, 1 failed", got.Verdict, got.Reading, want)
	}
}

// Like CSO 3.17.6: 7 slots on bundle ...0313 and the 2 newest on ...1613. The
// operator is keyed by its newest FBC's bundle, the 7 others are conflicts,
// not a 9-slot union, and the candidate is inconsistent, so it has no
// pullspecs.
func TestGetCandidateFBCMixed(t *testing.T) {
	srv := setupTestServer(t)
	const old, cur = "cso-3.17.6.202610120313", "cso-3.17.6.202610121613"
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.17.6-20261002100000.ocp4.12", "qo-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.17.6-20261002100000.ocp4.12", "qbo-bundle")
	cso := `container-security-operator "` + cur + `" 2/9`
	var conflicts string
	for minor := 12; minor <= 20; minor++ {
		nvr, bundle := fmt.Sprintf("container-security-operator-fbc-3.17.6-20261012040000.ocp4.%d", minor), old
		if minor >= 19 {
			nvr, bundle = fmt.Sprintf("container-security-operator-fbc-3.17.6-20261012170000.ocp4.%d", minor), cur
			cso += fmt.Sprintf(" 4.%d:%s", minor, nvr)
		} else {
			conflicts += fmt.Sprintf(" conflict=4.%d:%s", minor, nvr)
		}
		seedHistoryFBC(t, srv, nvr, bundle)
	}

	cand, _ := getFBC(t, srv, "quay-v3.17.6", "quay-3-17")
	checkRows(t, "candidate", fbcRows(cand), []string{
		"stream inconsistent [container-security-operator heads more than one bundle across its OCP versions]",
		`quay-operator "qo-bundle" 1/1 4.12:quay-operator-fbc-3.17.6-20261002100000.ocp4.12`,
		cso + conflicts,
		`quay-bridge-operator "qbo-bundle" 1/1 4.12:quay-bridge-operator-fbc-3.17.6-20261002100000.ocp4.12`,
	})
	if len(cand.Pullspecs) != 0 {
		t.Errorf("pullspecs %v, want none", cand.Pullspecs)
	}
}

// seedCatalogRun stores a quay-3-18 periodic run that installed from the
// catalog image pullspec.
func seedCatalogRun(t *testing.T, srv *Server, job, id, started, catalog string) {
	t.Helper()
	at := mustTime(t, started)
	run := &prow.Run{
		JobName: jobPrefix + job, BuildID: id, Application: "quay-3-18", State: "success", StartedAt: &at,
		ProwURL: "https://prow.example/" + id, ArtifactState: prow.ArtifactPresent, CatalogRef: catalog, FetchedAt: at,
	}
	if err := srv.db.UpsertProwRun(t.Context(), run); err != nil {
		t.Fatal(err)
	}
}

// A slot's CI is in two tiers: runs from its own digest, and runs from another
// FBC of its operator and OCP version on the same head bundle. A run from
// another slot, or from an FBC on another bundle, is neither.
func TestGetCandidateFBCEvidence(t *testing.T) {
	srv := setupTestServer(t)
	const head, older = "quay-operator-metadata-container-3.18.1.202610090339", "quay-operator-metadata-container-3.18.1.202610080000"
	ocp20 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.20", head)
	rebuilt := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009040000.ocp4.20", head)
	stale := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261008000000.ocp4.20", older)
	ocp21 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.21", head)
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.18.1-20261009044841.ocp4.20", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261009044841.ocp4.20", "qbo-bundle")
	seedCatalogRun(t, srv, "aws", "1", "2026-10-09T06:00:00Z", ocp20)
	seedCatalogRun(t, srv, "aws", "2", "2026-10-09T05:00:00Z", rebuilt)
	seedCatalogRun(t, srv, "gcp", "3", "2026-10-09T05:30:00Z", stale)
	seedCatalogRun(t, srv, "gcp", "4", "2026-10-09T07:00:00Z", ocp21)

	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	tier := func(jobs []model.CIJob) string {
		var ids []string
		for _, j := range jobs {
			for _, r := range j.Runs {
				ids = append(ids, strings.TrimPrefix(j.JobName, jobPrefix)+"="+strings.TrimPrefix(r.ProwURL, "https://prow.example/"))
			}
		}
		return strings.Join(ids, ",")
	}
	var got []string
	for _, o := range cand.Operators {
		for _, m := range o.Members {
			got = append(got, o.Operator+" "+m.OCP+" exact["+tier(m.CI.Exact)+"] equivalent["+tier(m.CI.Equivalent)+"]")
		}
	}
	checkRows(t, "ci", got, []string{
		"quay-operator 4.20 exact[aws=1] equivalent[aws=2]",
		"quay-operator 4.21 exact[gcp=4] equivalent[]",
		"container-security-operator 4.20 exact[] equivalent[]",
		"quay-bridge-operator 4.20 exact[] equivalent[]",
	})
}

// Like 3.16 on 12 Oct 2026, head bundles with no fbc-ri stage Release are not
// on stage; once each operator's succeeded, they are. A failed one is not.
func TestGetCandidateFBCRIStage(t *testing.T) {
	srv := setupTestServer(t)
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	heads := map[string]string{
		"quay-operator":               "quay-operator-metadata-container-3.18.1.202610090339",
		"container-security-operator": "container-security-operator-metadata-container-3.18.1.202610090339",
		"quay-bridge-operator":        "quay-bridge-operator-metadata-container-3.18.1.202610090339",
	}
	for _, operator := range artbuild.Operators {
		seedHistoryFBC(t, srv, operator+"-fbc-3.18.1-20261009044841.ocp4.20", heads[operator])
	}
	states := func(c *model.FBCCandidate) string {
		var s []string
		for _, o := range c.Operators {
			s = append(s, o.FBCRIStage.State)
		}
		return strings.Join(s, " ")
	}
	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	if got := states(cand); got != "none none none" {
		t.Errorf("without fbc-ri Releases: %s, want none none none", got)
	}

	for i, operator := range artbuild.Operators {
		label := "ri/" + operator
		resolveNVR(t, srv, label, heads[operator])
		snap := fmt.Sprintf("fbc-ri-stage-quay-3-18-%s-%d", operator, i)
		seedImages(t, srv, snap, "2026-10-09T05:00:00Z", map[string]string{"quay-3-18-" + operator + "-bundle": testImage(label)})
		seedKonfluxRelease(t, srv, snap+"-r", snap, "quay-advisory-stage-3-18", "Succeeded", "2026-10-09T05:00:00Z", "2026-10-09T05:10:00Z")
	}
	cand, _ = getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	if got := states(cand); got != "in in in" {
		t.Errorf("with fbc-ri Releases: %s, want in in in", got)
	}

	seedImages(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-x", "2026-10-09T06:00:00Z", map[string]string{"quay-3-18-quay-operator-bundle": testImage("ri/quay-operator")})
	seedKonfluxRelease(t, srv, "fbc-ri-stage-quay-3-18-quay-operator-x-r", "fbc-ri-stage-quay-3-18-quay-operator-x",
		"quay-advisory-stage-3-18", "Failed", "2026-10-09T06:00:00Z", "2026-10-09T06:10:00Z")
	// The bundle reached stage once, so it stays on stage.
	cand, _ = getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	if got := states(cand); got != "in in in" {
		t.Errorf("after a later failed fbc-ri Release: %s, want in in in", got)
	}
}

// Like 3.17.6's quay-operator and all of 5.0, head bundles that reached stage
// only through the version's image shipment, in no fbc-ri Snapshot, are on
// stage.
func TestGetCandidateFBCRIStageImageShipment(t *testing.T) {
	srv := setupTestServer(t)
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	images := map[string]string{}
	for _, operator := range artbuild.Operators {
		head := operator + "-metadata-container-3.18.1.202610090339"
		seedHistoryFBC(t, srv, operator+"-fbc-3.18.1-20261009044841.ocp4.20", head)
		label := "shipment/" + operator
		resolveNVR(t, srv, label, head)
		images["quay-3-18-"+operator+"-bundle"] = testImage(label)
	}
	snap := "quay-stage-3-18-1-image-20261008082443"
	seedImages(t, srv, snap, "2026-10-08T08:24:43Z", images)
	seedKonfluxRelease(t, srv, snap, snap, "quay-advisory-stage-3-18", "Succeeded", "2026-10-08T08:24:43Z", "2026-10-08T08:40:00Z")

	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	var got []string
	for _, o := range cand.Operators {
		got = append(got, o.FBCRIStage.State)
	}
	if strings.Join(got, " ") != "in in in" {
		t.Errorf("with an image-shipment Release: %v, want in in in", got)
	}
}

// Each operator's head bundle is checked against the image Snapshot's bundle
// of it, which never keys the candidate.
func TestGetCandidateFBCImageCheck(t *testing.T) {
	srv := setupTestServer(t)
	const head = "quay-operator-metadata-container-3.18.1.202610090339.p2.g35cf767.assembly.stream.el9-1"
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009044841.ocp4.20", head)
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.18.1-20261009044841.ocp4.20", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261009044841.ocp4.20", "qbo-bundle")
	checks := func(c *model.FBCCandidate) []string {
		var out []string
		for _, o := range c.Operators {
			out = append(out, o.Operator+" "+o.ImageCheck.State+" "+o.ImageCheck.Snapshot+" "+o.ImageCheck.BundleNVR)
		}
		return out
	}
	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	checkRows(t, "no image Snapshot", checks(cand), []string{
		"quay-operator absent  ", "container-security-operator absent  ", "quay-bridge-operator absent  ",
	})

	const snap = "quay-3-18-20261009-042610-000"
	const cso = "container-security-operator-metadata-container-3.18.1.202610080000.p2.g1.assembly.stream.el9-1"
	seedImages(t, srv, snap, "2026-10-09T04:29:06Z", testBuild("img", "quay-3-18-quay-operator-bundle", "quay-3-18-container-security-operator-bundle", "quay-3-18-quay-bridge-operator-bundle"))
	resolveNVR(t, srv, "img/quay-3-18-quay-operator-bundle", head)
	resolveNVR(t, srv, "img/quay-3-18-container-security-operator-bundle", cso)
	cand, _ = getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	checkRows(t, "newest build", checks(cand), []string{
		"quay-operator matches " + snap + " " + head,
		"container-security-operator differs " + snap + " " + cso,
		"quay-bridge-operator absent " + snap + " ",
	})
	if cand.State != "complete" || cand.Operators[0].HeadBundleNVR != head {
		t.Errorf("state %s, quay-operator head %q: the image check must not change the candidate", cand.State, cand.Operators[0].HeadBundleNVR)
	}
}

// Like 5.0.0's image shipment: it holds the CSO bundle as component
// container-security-operator-bundle, without the application prefix, so an
// older bundle there differs.
func TestGetCandidateFBCImageCheckByNVR(t *testing.T) {
	const cso = "container-security-operator-metadata-container-3.18.1.202610090339.p2.g1.assembly.stream.el9-1"
	const older = "container-security-operator-metadata-container-3.18.1.202610010000.p2.g1.assembly.stream.el9-1"
	for _, tc := range []struct {
		nvrs        map[string]string // component -> bundle NVR
		state, want string
	}{
		{map[string]string{"container-security-operator-bundle": cso}, "matches", cso},
		{map[string]string{"container-security-operator-bundle": older}, "differs", older},
		// The head bundle wins over an older one of the same name.
		{map[string]string{"container-security-operator-bundle": older, "quay-3-18-container-security-operator-bundle": cso}, "matches", cso},
	} {
		srv := setupTestServer(t)
		seedHistoryFBC(t, srv, "container-security-operator-fbc-3.18.1-20261009044841.ocp4.20", cso)
		const snap = "quay-3-18-20261009-042610-000"
		var comps []string
		for c := range tc.nvrs {
			comps = append(comps, c)
		}
		seedImages(t, srv, snap, "2026-10-09T04:29:06Z", testBuild("img", comps...))
		for c, nvr := range tc.nvrs {
			resolveNVR(t, srv, "img/"+c, nvr)
		}
		cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
		if c := cand.Operators[1].ImageCheck; c.State != tc.state || c.BundleNVR != tc.want {
			t.Errorf("%s image check %+v, want %s %s", cand.Operators[1].Operator, c, tc.state, tc.want)
		}
	}
}

// Like 3.17.6 against 3.17.5: ART renamed every relatedImage, and moved the
// operator to another repository under the same name.
func TestCompareImagesByRepository(t *testing.T) {
	pg := "registry.redhat.io/rhel8/postgresql-10@sha256:eec360a32519"
	from := []model.OperatorImage{
		{Name: "postgres_previous", Image: pg, Digest: "sha256:eec360a32519"},
		{Name: "operator", Image: "registry.redhat.io/quay/quay-operator-rhel9@sha256:a1", Digest: "sha256:a1"},
	}
	to := []model.OperatorImage{
		{Name: "postgresql-10", Image: pg, Digest: "sha256:eec360a32519"},
		{Name: "operator", Image: "registry.redhat.io/quay/quay-rhel9-operator@sha256:b2", Digest: "sha256:b2"},
	}
	var got []string
	for _, r := range compareImages(from, to) {
		got = append(got, r.Name+" "+r.From.Repository+" "+r.To.Repository+" "+r.Change)
	}
	checkRows(t, "compare", got, []string{
		"postgresql-10 registry.redhat.io/rhel8/postgresql-10 registry.redhat.io/rhel8/postgresql-10 same",
		"operator registry.redhat.io/quay/quay-operator-rhel9 registry.redhat.io/quay/quay-rhel9-operator renamed",
	})
}

// seedImagesFBC stores ART's successful build of the FBC image of nvr,
// rendered with its channel headed by the bundle of NVR bundle, whose
// relatedImages are head, above the entry of version previousVersion, whose
// bundle's are previous. It returns the image's pullspec.
func seedImagesFBC(t *testing.T, srv *Server, nvr, bundle, previousVersion string, head, previous []fbc.RelatedImage) string {
	t.Helper()
	ctx := t.Context()
	pullspec, bundleImage := testImage("fbc/"+nvr), testImage("bundle/"+bundle)
	for _, b := range []artbuild.HistoryBuild{
		{RecordID: "rec-" + nvr, Group: "quay-3.18", Type: "fbc", NVR: nvr, Digest: testDigest("fbc/" + nvr), Pullspec: pullspec},
		{RecordID: "rec-" + bundle, Group: "quay-3.18", Type: "bundle", NVR: bundle, Digest: testDigest("bundle/" + bundle), Pullspec: bundleImage},
	} {
		if err := srv.db.UpsertArtHistoryBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.db.UpsertFBCRender(ctx, fbc.Render{
		Digest: imageDigest(pullspec), State: fbc.StateRendered, RenderedAt: time.Now(),
		Channel: fbc.Channel{
			Name: "stable-3.18", HeadCSV: "csv-" + bundle, HeadBundlePullspec: bundleImage, HeadRelatedImages: head,
			Previous: fbc.Bundle{Version: previousVersion, RelatedImages: previous},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return pullspec
}

// Like 3.17.6's quay-operator: the staged bundle's images, joined to ART's
// builds by digest, compared with the previous z's and with the newer bundle
// the stream heads.
func TestGetCandidateFBCImages(t *testing.T) {
	srv := setupTestServer(t)
	srv.StageReleasePlanPattern = regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	ctx := t.Context()
	art := func(label, commit string) fbc.RelatedImage {
		if err := srv.db.UpsertArtHistoryBuild(ctx, artbuild.HistoryBuild{
			RecordID: "rec-" + label, Group: "quay-3.18", Type: "image", NVR: label + "-nvr",
			Digest: testDigest(label), Pullspec: testImage(label), Commitish: commit,
		}); err != nil {
			t.Fatal(err)
		}
		return fbc.RelatedImage{Image: testImage(label)}
	}
	named := func(name string, img fbc.RelatedImage) fbc.RelatedImage {
		img.Name = name
		return img
	}
	// redis and postgresql-10 are external: ART has no build of them.
	redis := fbc.RelatedImage{Name: "redis", Image: "registry.redhat.io/rhel9/redis-7@" + testDigest("redis")}
	previous := []fbc.RelatedImage{
		named("operator", art("prev/operator", "c1")),
		named("quay", art("prev/quay", "q1")),
		{Name: "clair", Image: testImage("prev/clair")},
		redis,
		{Name: "postgresql-10", Image: testImage("pg10")},
	}
	staged := []fbc.RelatedImage{
		named("operator", art("a/operator", "c1")),
		named("quay", art("a/quay", "q2")),
		named("clair", art("a/clair", "k1")),
		redis,
		named("builder", art("a/builder", "b1")),
	}
	newer := []fbc.RelatedImage{
		named("operator", art("b/operator", "c1")),
		named("quay", art("b/quay", "q3")),
		named("clair", art("a/clair", "k1")),
		redis,
		named("builder", art("a/builder", "b1")),
	}
	const bundleA, bundleB = "quay-operator-metadata-container-3.18.1.202610060936", "quay-operator-metadata-container-3.18.1.202610122028"
	fbcA := seedImagesFBC(t, srv, "quay-operator-fbc-3.18.1-20261006151933.ocp4.21", bundleA, "3.18.0", staged, previous)
	seedImagesFBC(t, srv, "quay-operator-fbc-3.18.1-20261012203000.ocp4.21", bundleB, "3.18.0", newer, previous)
	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "fbc-quay-3-18-a", "2026-10-07T00:00:00Z", []string{fbcA}, "Succeeded")

	// A stage Release ships quay and a prod Release redis.
	seedImages(t, srv, "quay-stage-3-18-1", "2026-10-07T00:00:00Z", map[string]string{"quay-3-18-quay-quay": testImage("a/quay")})
	seedKonfluxRelease(t, srv, "stage-r", "quay-stage-3-18-1", "quay-advisory-stage-3-18", "Succeeded", "2026-10-07T00:00:00Z", "2026-10-07T00:10:00Z")
	seedImages(t, srv, "quay-prod-3-18-0", "2026-09-07T00:00:00Z", map[string]string{"quay-3-18-redis": redis.Image})
	if err := srv.db.UpsertStagedSnapshot(ctx, "quay-prod-3-18-0", "3.18.0", "image", "prod"); err != nil {
		t.Fatal(err)
	}
	seedKonfluxRelease(t, srv, "prod-r", "quay-prod-3-18-0", "quay-advisory-prod-3-18", "Succeeded", "2026-09-07T00:00:00Z", "2026-09-07T00:10:00Z")

	// Only quay's digest has a resolved ART build; the other ART images link
	// their history record.
	resolveNVR(t, srv, "a/quay", "a/quay-nvr")

	cand, stream := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	o := cand.Operators[0]
	var images []string
	for _, img := range o.Images {
		images = append(images, fmt.Sprintf("%s %q %q page=%t stage=%t prod=%t", img.Name, img.NVR, img.Commit, img.BuildURL != "", img.Stage, img.Prod))
	}
	checkRows(t, "images", images, []string{
		`builder "a/builder-nvr" "b1" page=true stage=false prod=false`,
		`clair "a/clair-nvr" "k1" page=true stage=false prod=false`,
		`operator "a/operator-nvr" "c1" page=true stage=false prod=false`,
		`quay "a/quay-nvr" "q2" page=true stage=true prod=false`,
		`redis "" "" page=false stage=false prod=true`,
	})
	if got, want := o.Images[1].BuildURL, artbuild.PageURL(srv.artBaseURL, "a/clair-nvr", "rec-a/clair"); got != want {
		t.Errorf("clair build_url %q, want %q", got, want)
	}
	compare := func(c model.ImageCompare) []string {
		rows := []string{c.Kind + " " + c.Label}
		ref := func(r *model.ImageRef) string {
			if r == nil {
				return "-"
			}
			if r.BuildURL != "" {
				return r.NVR + "@" + r.Commit + "+page"
			}
			return r.NVR + "@" + r.Commit
		}
		for _, r := range c.Rows {
			rows = append(rows, r.Name+" "+ref(r.From)+" "+ref(r.To)+" "+r.Change)
		}
		return rows
	}
	if len(o.Compare) != 2 {
		t.Fatalf("compare %+v, want previous and newer", o.Compare)
	}
	checkRows(t, "previous", compare(o.Compare[0]), []string{
		"previous 3.18.0",
		"builder - a/builder-nvr@b1+page added",
		"clair @ a/clair-nvr@k1+page new build",
		"operator prev/operator-nvr@c1+page a/operator-nvr@c1+page rebuild",
		"quay prev/quay-nvr@q1+page a/quay-nvr@q2+page new source",
		"redis @ @ same",
		"postgresql-10 @ - removed",
	})
	checkRows(t, "newer", compare(o.Compare[1]), []string{
		"newer " + bundleB,
		"builder a/builder-nvr@b1+page a/builder-nvr@b1+page same",
		"clair a/clair-nvr@k1+page a/clair-nvr@k1+page same",
		"operator a/operator-nvr@c1+page b/operator-nvr@c1+page rebuild",
		"quay a/quay-nvr@q2+page b/quay-nvr@q3+page new source",
		"redis @ @ same",
	})
	// The stream heads the newer bundle: it has only the previous compare.
	if s := stream.Operators[0]; len(s.Compare) != 1 || s.Compare[0].Kind != "previous" || s.Images[0].Digest != testDigest("a/builder") {
		t.Errorf("stream compare %+v, images %+v", s.Compare, s.Images)
	}
	// An operator without an FBC has no images.
	if c := cand.Operators[1]; c.Images == nil || len(c.Images) != 0 || len(c.Compare) != 0 {
		t.Errorf("%s images %+v, compare %+v, want none", c.Operator, c.Images, c.Compare)
	}
}

// seedScanFBC stores 3.18.1's staged quay-operator FBC, whose head bundle's
// relatedImages are operator, quay and clair, and returns their digests.
func seedScanFBC(t *testing.T, srv *Server) []string {
	t.Helper()
	var head []fbc.RelatedImage
	var digests []string
	for _, name := range []string{"operator", "quay", "clair"} {
		head = append(head, fbc.RelatedImage{Name: name, Image: testImage("a/" + name)})
		digests = append(digests, testDigest("a/"+name))
	}
	img := seedImagesFBC(t, srv, "quay-operator-fbc-3.18.1-20261006151933.ocp4.21", "quay-operator-metadata-container-3.18.1.202610060936", "3.18.0", head, nil)
	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "fbc-quay-3-18-a", "2026-10-07T00:00:00Z", []string{img}, "Succeeded")
	return digests
}

// An Images row's scan is its image's stored scan, null without one.
func TestGetCandidateScan(t *testing.T) {
	srv := setupTestServer(t)
	seedScanFBC(t, srv)
	const page = "https://konflux-ui.example/ns/art-quay-tenant/applications/quay-3-18/pipelineruns/"
	for _, sc := range []scan.Scan{
		{
			Digest: testDigest("a/operator"), State: scan.StateScanned,
			PipelineRun: "quay-3-18-quay-operator-7mbwk", DetailURL: page + "quay-3-18-quay-operator-7mbwk",
			Counts:  &model.ScanCounts{Basis: scan.BasisArchFindings, NoFix: model.SeverityCounts{High: 24, Medium: 264, Low: 232}},
			Reports: map[string]string{"sha256:arch": "sha256:report"},
		},
		{
			Digest: testDigest("a/quay"), State: scan.StateScanFailed,
			PipelineRun: "quay-3-18-quay-quay-zwvxj", DetailURL: page + "quay-3-18-quay-quay-zwvxj",
		},
	} {
		sc.CheckedAt = time.Now()
		if err := srv.db.UpsertImageScan(t.Context(), sc); err != nil {
			t.Fatal(err)
		}
	}

	cand, _ := getFBC(t, srv, "quay-v3.18.1", "quay-3-18")
	want := map[string]*model.ImageScan{
		"operator": {
			State: "scanned", URL: page + "quay-3-18-quay-operator-7mbwk",
			Counts: &model.ScanCounts{Basis: "scanner_arch_findings", NoFix: model.SeverityCounts{High: 24, Medium: 264, Low: 232}},
		},
		"quay":  {State: "scan_failed", URL: page + "quay-3-18-quay-quay-zwvxj"},
		"clair": nil,
	}
	images := cand.Operators[0].Images
	if len(images) != len(want) {
		t.Fatalf("images %+v, want %d", images, len(want))
	}
	for _, img := range images {
		if !reflect.DeepEqual(img.Scan, want[img.Name]) {
			t.Errorf("%s scan = %+v, want %+v", img.Name, img.Scan, want[img.Name])
		}
	}
}

// The scan sync reads the images the Images rows of every unshipped version
// not archived show, once each; a released or archived version reads none.
func TestCandidateDigests(t *testing.T) {
	srv := setupTestServer(t)
	want := seedScanFBC(t, srv)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}

	got, err := srv.CandidateDigests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("digests = %v, want %v", got, want)
	}

	for _, v := range []model.ReleaseVersion{
		{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Released: true},
		{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18", Archived: true},
	} {
		if err := srv.db.UpsertReleaseVersion(t.Context(), &v); err != nil {
			t.Fatal(err)
		}
		if got, err := srv.CandidateDigests(t.Context()); err != nil || len(got) != 0 {
			t.Errorf("released %t, archived %t: digests = %v, %v; want none", v.Released, v.Archived, got, err)
		}
	}
}

// A slot's CI is the candidate's FBC's, runs from its digest and from another
// FBC of the slot on its head bundle, never another FBC's own; a slot with no
// run still has its CI, and one without an FBC in the candidate has none.
func TestGetCandidateCIHistory(t *testing.T) {
	srv := setupTestServer(t)
	const a = "quay-operator-metadata-container-3.18.1.a"
	fbcs := map[int]string{}
	for day := 1; day <= 4; day++ {
		fbcs[day] = seedHistoryFBC(t, srv, fmt.Sprintf("quay-operator-fbc-3.18.1-2026100%d000000.ocp4.20", day), a)
	}
	op21 := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261001000000.ocp4.21", a)
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261002000000.ocp4.21", a)
	for day := 1; day <= 2; day++ {
		seedHistoryFBC(t, srv, fmt.Sprintf("quay-operator-fbc-3.18.1-2026100%d000000.ocp4.22", day), a)
	}
	seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261001000000.ocp4.23", a)
	op23 := testImage("fbc/unbuilt")
	seedRender(t, srv, op23, "quay-operator-fbc-3.18.1-20261002000000.ocp4.23", a)
	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "quay-stage-3-18-1-fbc-20261002000000", "2026-10-02T00:00:00Z", []string{fbcs[2], op21, op23}, "Succeeded")
	seedCatalogRun(t, srv, "aws", "1", "2026-10-02T06:00:00Z", fbcs[2])
	seedCatalogRun(t, srv, "gcp", "2", "2026-10-03T06:00:00Z", fbcs[3])
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}

	var got model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &got)
	tier := func(jobs []model.CIJob) string {
		var ids []string
		for _, j := range jobs {
			for _, r := range j.Runs {
				ids = append(ids, strings.TrimPrefix(j.JobName, jobPrefix)+"="+strings.TrimPrefix(r.ProwURL, "https://prow.example/"))
			}
		}
		return strings.Join(ids, ",")
	}
	var rows []string
	for _, s := range got.CIHistory {
		row := s.Operator + " " + s.OCP + " -"
		if s.CI != nil {
			row = fmt.Sprintf("%s %s [%s|%s]", s.Operator, s.OCP, tier(s.CI.Exact), tier(s.CI.Equivalent))
		}
		rows = append(rows, row)
	}
	checkRows(t, "ci history", rows, []string{
		"quay-operator 4.20 [aws=1|gcp=2]",
		"quay-operator 4.21 [|]",
		"quay-operator 4.22 -",
		"quay-operator 4.23 [|]",
	})
}

// CI is advisory: a failed run on the candidate's FBC leaves the verdict as
// it was.
func TestGetCandidateCIAdvisory(t *testing.T) {
	srv := setupTestServer(t)
	scanHistory(t, srv, "quay-3.18")
	op := seedHistoryFBC(t, srv, "quay-operator-fbc-3.18.1-20261009000000.ocp4.20", "qo-bundle")
	seedHistoryFBC(t, srv, "container-security-operator-fbc-3.18.1-20261009000000.ocp4.20", "cso-bundle")
	seedHistoryFBC(t, srv, "quay-bridge-operator-fbc-3.18.1-20261009000000.ocp4.20", "qbo-bundle")
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}
	var before, after model.ReleaseCandidate
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &before)

	at := mustTime(t, "2026-10-09T06:00:00Z")
	if err := srv.db.UpsertProwRun(t.Context(), &prow.Run{
		JobName: jobPrefix + "aws", BuildID: "1", Application: "quay-3-18", State: "failure", StartedAt: &at,
		ProwURL: "https://prow.example/1", ArtifactState: prow.ArtifactPresent, CatalogRef: op, FetchedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/candidate", http.StatusOK, &after)
	if s := after.CIHistory[0]; s.CI == nil || len(s.CI.Exact) != 1 || s.CI.Exact[0].Runs[0].State != "failure" {
		t.Fatalf("quay-operator 4.20 CI %+v, want the failed run", s)
	}
	if !reflect.DeepEqual(after.Verdict, before.Verdict) || after.Reason != before.Reason {
		t.Errorf("verdict %+v, reason %q after a failed run; want %+v, %q", after.Verdict, after.Reason, before.Verdict, before.Reason)
	}
}
