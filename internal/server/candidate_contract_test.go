package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
)

// seedContractFBC stores the quay-operator FBC of nvr, its head bundle's
// relatedImages the images labelled labels, staged in app's FBC Snapshot for
// version unless app is "", and returns their digests.
func seedContractFBC(t *testing.T, srv *Server, app, version, nvr string, labels ...string) []string {
	t.Helper()
	var head []fbc.RelatedImage
	var digests []string
	for _, l := range labels {
		head = append(head, fbc.RelatedImage{Name: l, Image: testImage(l)})
		digests = append(digests, testDigest(l))
	}
	img := seedImagesFBC(t, srv, nvr, "bundle-"+nvr, "", head, nil)
	if app != "" {
		seedStagedFBC(t, srv, "fbc-"+app, version, "fbc-"+app+"-"+version, "2026-10-07T00:00:00Z", []string{img}, "Succeeded")
	}
	return digests
}

// The scan sync reads exactly the digests the candidate API's Images rows show
// across fbc_candidate and fbc_stream of every unshipped version not archived.
func TestCandidateDigestsContract(t *testing.T) {
	srv := setupTestServer(t)
	ctx := t.Context()
	for _, v := range []model.ReleaseVersion{
		{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"},
		{Name: "quay-v3.17.6", KonfluxApplication: "quay-3-17"},
		{Name: "quay-v3.16.9", KonfluxApplication: "quay-3-16", Released: true},
		{Name: "quay-v3.15.4", KonfluxApplication: "quay-3-15", Archived: true},
	} {
		if err := srv.db.UpsertReleaseVersion(ctx, &v); err != nil {
			t.Fatal(err)
		}
	}
	// 3.18.1's stage heads S; its stream's newer build heads T, sharing quay.
	s := seedContractFBC(t, srv, "quay-3-18", "3.18.1", "quay-operator-fbc-3.18.1-20261006151933.ocp4.21", "s/operator", "st/quay")
	tt := seedContractFBC(t, srv, "", "3.18.1", "quay-operator-fbc-3.18.1-20261012203000.ocp4.21", "t/operator", "st/quay")
	u := seedContractFBC(t, srv, "quay-3-17", "3.17.6", "quay-operator-fbc-3.17.6-20261006151933.ocp4.21", "u/operator", "u/quay")
	excluded := slices.Concat(
		seedContractFBC(t, srv, "quay-3-16", "3.16.9", "quay-operator-fbc-3.16.9-20261006151933.ocp4.21", "released/operator"),
		seedContractFBC(t, srv, "quay-3-15", "3.15.4", "quay-operator-fbc-3.15.4-20261006151933.ocp4.21", "archived/operator"),
	)

	got, err := srv.CandidateDigests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := slices.Compact(slices.Sorted(slices.Values(slices.Concat(s, tt, u))))
	if !slices.Equal(got, want) {
		t.Errorf("CandidateDigests = %v, want %v", got, want)
	}

	images := func(c *model.FBCCandidate) []string {
		if c == nil {
			return nil
		}
		var digests []string
		for _, o := range c.Operators {
			for _, img := range o.Images {
				if img.Digest != "" {
					digests = append(digests, img.Digest)
				}
			}
		}
		slices.Sort(digests)
		return digests
	}
	versions, err := srv.db.ListAllReleaseVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var shown, other []string
	for _, v := range versions {
		var c model.ReleaseCandidate
		getJSON(t, srv, "/api/v1/releases/"+v.Name+"/candidate", http.StatusOK, &c)
		if v.Name == "quay-v3.18.1" {
			if c.FBCCandidate == nil || c.FBCCandidate.Source != "stage_snapshots" || c.FBCStream == nil || c.FBCStream.Source != "stream" {
				t.Fatalf("3.18.1 fbc_candidate %+v, fbc_stream %+v; want stage and stream", c.FBCCandidate, c.FBCStream)
			}
			checkRows(t, "3.18.1 fbc_candidate images", images(c.FBCCandidate), slices.Sorted(slices.Values(s)))
			checkRows(t, "3.18.1 fbc_stream images", images(c.FBCStream), slices.Sorted(slices.Values(tt)))
		}
		if c.Shipped || v.Archived {
			other = append(other, images(c.FBCCandidate)...)
			other = append(other, images(c.FBCStream)...)
			continue
		}
		shown = append(shown, images(c.FBCCandidate)...)
		shown = append(shown, images(c.FBCStream)...)
	}
	slices.Sort(shown)
	if shown = slices.Compact(shown); !slices.Equal(got, shown) {
		t.Errorf("CandidateDigests = %v, candidate Images rows show %v", got, shown)
	}
	slices.Sort(other)
	if !slices.Equal(other, slices.Sorted(slices.Values(excluded))) {
		t.Errorf("released and archived Images rows %v, want %v", other, excluded)
	}
	for _, d := range other {
		if slices.Contains(got, d) {
			t.Errorf("CandidateDigests has %s of a released or archived version", d)
		}
	}
}
