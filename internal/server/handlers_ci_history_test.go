package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/release"
)

// The CI history lists the version's FBC builds newest built first, by
// state: shipped, the candidate's, staged, or none; each with the runs that
// installed from it, newest first.
func TestGetReleaseCI(t *testing.T) {
	srv := setupTestServer(t)
	const bundle = "quay-operator-metadata-container-3.18.1.a"
	built := func(nvr, start string) string {
		pullspec := seedHistoryFBC(t, srv, nvr, bundle)
		if err := srv.db.UpsertArtHistoryBuild(t.Context(), artbuild.HistoryBuild{
			RecordID: "rec-" + nvr, Group: "quay-3.18", Type: "fbc", NVR: nvr, Digest: release.ImageDigest(pullspec), Pullspec: pullspec, StartTime: start,
		}); err != nil {
			t.Fatal(err)
		}
		return pullspec
	}
	shipped := built("quay-operator-fbc-3.18.1-20261001000000.ocp4.20", "2026-10-01T00:10:00")
	staged := built("quay-operator-fbc-3.18.1-20261002000000.ocp4.20", "2026-10-02T00:10:00")
	cand := built("quay-operator-fbc-3.18.1-20261003000000.ocp4.20", "2026-10-03T00:10:00")
	built("container-security-operator-fbc-3.18.1-20261003120000.ocp4.20", "2026-10-03T12:10:00")
	tested := built("quay-operator-fbc-3.18.1-20261004000000.ocp4.20", "2026-10-04T00:10:00")
	built("quay-operator-fbc-3.18.2-20261005000000.ocp4.20", "2026-10-05T00:10:00")

	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "quay-prod-3-18-1-fbc-20261001", "2026-10-01T01:00:00Z", []string{shipped}, "Succeeded")
	if err := srv.db.UpsertStagedSnapshot(t.Context(), "quay-prod-3-18-1-fbc-20261001", "3.18.1", "fbc", "prod"); err != nil {
		t.Fatal(err)
	}
	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "quay-stage-3-18-1-fbc-20261002", "2026-10-02T01:00:00Z", []string{staged}, "Succeeded")
	seedStagedFBC(t, srv, "fbc-quay-3-18", "3.18.1", "quay-stage-3-18-1-fbc-20261003", "2026-10-03T01:00:00Z", []string{cand}, "Failed", "Succeeded")
	seedCatalogRun(t, srv, "aws-ocp422-e2e-install-aws-s3-nightly", "1", "2026-10-05T06:00:00Z", tested)
	seedCatalogRun(t, srv, "gcp", "2", "2026-10-04T06:00:00Z", tested)
	seedCatalogRun(t, srv, "aws", "3", "2026-10-01T06:00:00Z", shipped)
	if err := srv.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}); err != nil {
		t.Fatal(err)
	}

	var got []model.FBCBuild
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.1/ci", http.StatusOK, &got)
	var rows []string
	for _, b := range got {
		var runs []string
		for _, r := range b.Runs {
			runs = append(runs, strings.TrimPrefix(r.ProwURL, "https://prow.example/"))
		}
		rows = append(rows, fmt.Sprintf("%s %s %s %s [%s]", b.Operator, b.OCP, b.BuiltAt.Format("01-02T15"), b.State, strings.Join(runs, ",")))
	}
	checkRows(t, "ci", rows, []string{
		"quay-operator 4.20 10-04T00  [1,2]",
		"container-security-operator 4.20 10-03T12  []",
		"quay-operator 4.20 10-03T00 candidate []",
		"quay-operator 4.20 10-02T00 staged []",
		"quay-operator 4.20 10-01T00 shipped [3]",
	})
	const sippy = "https://sippy.dptools.openshift.org/sippy-ng/jobs/quay-3.18?filters=%7B%22items%22%3A%5B%7B%22columnField%22%3A%22name%22%2C%22operatorValue%22%3A%22equals%22%2C%22value%22%3A%22periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly%22%7D%5D%7D"
	if r := got[0].Runs[0]; r.SippyURL != sippy || r.Job != jobPrefix+"aws-ocp422-e2e-install-aws-s3-nightly" || r.State != "success" {
		t.Errorf("run 1 %+v, want Sippy URL %s", r, sippy)
	}

	getJSON(t, srv, "/api/v1/releases/quay-v9.9.9/ci", http.StatusNotFound, nil)
}
