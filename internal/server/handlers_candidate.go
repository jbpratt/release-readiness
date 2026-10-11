package server

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

// quayVersion is a concrete version, named after its ART assembly:
// quay-v3.18.1 is assembly 3.18.1.
var quayVersion = regexp.MustCompile(`^quay-v(\d+\.\d+\.\d+)$`)

// nvrXYZ is the X.Y.Z an NVR's version field starts with, which ends at a dot
// or at the field's end: 3.18.10 is not 3.18.1.
var nvrXYZ = regexp.MustCompile(`^(\d+\.\d+\.\d+)(?:\.|$)`)

// handleGetCandidate serves what decides whether an unshipped version can
// ship: its release candidate, the STAGE build, with whether it reached stage,
// the version's prod Release and the periodic jobs that installed its operator
// bundle, and the version's newest build when it differs from the candidate or
// there is none.
func (s *Server) handleGetCandidate(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	resp, err := s.releaseCandidate(r.Context(), release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) releaseCandidate(ctx context.Context, release *model.ReleaseVersion) (*model.ReleaseCandidate, error) {
	c, err := s.selectCandidate(ctx, release)
	if err != nil {
		return nil, err
	}
	resp := &model.ReleaseCandidate{Shipped: c.shipped, Reason: c.reason}
	if c.assembly == "" {
		return resp, nil
	}
	app := release.KonfluxApplication
	releases, err := s.db.StageReleases(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		return nil, err
	}
	newest := func(images []model.SnapshotImage) *model.NewestBuild {
		return &model.NewestBuild{Snapshot: c.newest.Name, CreatedAt: c.newest.CreatedAt, Stage: stageState(images, releases)}
	}
	if c.build == nil || c.build.Source != "staged" {
		resp.Reason = "no release candidate yet"
		if c.newest != nil {
			resp.Newest = newest(buildImages(app, c.newest))
		}
		return resp, nil
	}

	cand := c.build
	resp.Candidate = cand
	held := map[string]string{}
	for _, cc := range cand.Components {
		held[cc.Name] = imageDigest(cc.Image)
	}
	// Its own stage Release succeeded with every image of it, and dates it:
	// ART assembles a STAGE build from images it often staged before.
	cand.Stage = model.StageFlag{State: "staged", StagedAt: c.stagedAt, Total: len(cand.Components), NotStaged: []string{}, NoBundle: []string{}, NoRelease: []string{}}
	if cand.Prod, err = s.db.LatestProdRelease(ctx, c.assembly); err != nil {
		return nil, err
	}
	cand.CI = []model.CIJob{}
	if bundle := held[app+"-quay-operator-bundle"]; bundle != "" {
		runs, err := s.db.ListProwRunsByApplication(ctx, app)
		if err != nil {
			return nil, err
		}
		cand.CI = ciJobs(runs, bundle)
	}
	if c.newest == nil {
		return resp, nil
	}
	// Only the components the candidate holds count: a stream builds images
	// its STAGE build can leave out.
	var imgs []model.SnapshotImage
	differs := false
	for _, img := range buildImages(app, c.newest) {
		if d, ok := held[img.Name]; ok {
			imgs = append(imgs, img)
			differs = differs || imageDigest(img.Image) != d
		}
	}
	if differs {
		resp.Newest = newest(imgs)
	}
	return resp, nil
}

// candidate is a version's build up for release. A version that shipped, or
// that is not one ART builds, has no assembly and nothing more to decide.
type candidate struct {
	shipped bool
	// reason says why the version has no build.
	reason string
	// build is the version's selected STAGE build, else newest, its newest
	// build: the first of its stream builds, newest first, as versionBuilds
	// selects them for its assembly. stagedAt is the STAGE build's stage
	// Release's completion time.
	build    *model.CandidateBuild
	stagedAt *time.Time
	newest   *model.ReleaseSnapshot
	assembly string
}

// selectCandidate selects the version's build up for release, for its page
// and the image scan sync alike.
func (s *Server) selectCandidate(ctx context.Context, release *model.ReleaseVersion) (*candidate, error) {
	app := release.KonfluxApplication
	m := quayVersion.FindStringSubmatch(release.Name)
	switch {
	case release.Released || s.catalogShipped(release.Name):
		return &candidate{shipped: true}, nil
	case app == "":
		return &candidate{reason: "no Konflux application"}, nil
	case m == nil:
		return &candidate{reason: "not a concrete quay-vX.Y.Z version"}, nil
	}
	builds, nvrs, err := s.db.StreamBuilds(ctx, app)
	if err != nil {
		return nil, err
	}
	c := &candidate{assembly: m[1]}
	if vb := versionBuilds(app, builds, nvrs, m[1]); len(vb) > 0 {
		c.newest = &vb[0]
	}
	if c.build, c.stagedAt, err = s.candidateBuild(ctx, release, c.newest); err != nil {
		return nil, err
	}
	if c.build == nil {
		c.reason = "no build of " + m[1] + " yet"
	}
	return c, nil
}

// versionBuilds returns, of an application's stream builds, newest first,
// those of version: whose buildVersion is version and after which ART built
// no lower version. ART can go back to a lower version; the builds before
// that were premature.
func versionBuilds(app string, builds []model.ReleaseSnapshot, nvrs map[string]string, version string) []model.ReleaseSnapshot {
	var out []model.ReleaseSnapshot
	for i := range builds {
		switch v := buildVersion(app, &builds[i], nvrs); {
		case v == version:
			out = append(out, builds[i])
		case v != "" && lowerVersion(v, version):
			return out
		}
	}
	return out
}

// lowerVersion reports whether X.Y.Z version a is numerically lower than b:
// 3.18.9 is lower than 3.18.10.
func lowerVersion(a, b string) bool {
	return slices.CompareFunc(strings.Split(a, "."), strings.Split(b, "."), func(x, y string) int {
		i, _ := strconv.Atoi(x)
		j, _ := strconv.Atoi(y)
		return cmp.Compare(i, j)
	}) < 0
}

// buildVersion returns the version of a stream build: the X.Y.Z that the NVR
// of every image of it ART resolved, base image excluded, carries. It is ""
// when ART resolved none or they differ, as while ART switches the stream to
// another version. nvrs holds ART's resolved NVRs by image.
func buildVersion(app string, snap *model.ReleaseSnapshot, nvrs map[string]string) string {
	version := ""
	for _, img := range buildImages(app, snap) {
		nvr, ok := nvrs[img.Image]
		if !ok {
			continue
		}
		v := nvrVersion(nvr)
		if v == "" || (version != "" && v != version) {
			return ""
		}
		version = v
	}
	return version
}

// nvrVersion returns the X.Y.Z that an NVR's version field, its second-to-last
// dash-separated field, starts with, or "". An image's and a bundle's are
// 3.18.1:
//
//	quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9
//	quay-operator-metadata-container-3.18.1.202610061637.p2.g35cf767.assembly.stream.el9-1
func nvrVersion(nvr string) string {
	f := strings.Split(nvr, "-")
	if len(f) < 2 {
		return ""
	}
	if m := nvrXYZ.FindStringSubmatch(f[len(f)-2]); m != nil {
		return m[1]
	}
	return ""
}

// CandidateDigests returns the image digests of the release candidate, the
// selected STAGE build, of every version that is not archived: the page shows
// the CVEs of no other build. Released versions select none.
func (s *Server) CandidateDigests(ctx context.Context) ([]string, error) {
	versions, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	var digests []string
	for _, v := range versions {
		if v.Archived {
			continue
		}
		c, err := s.selectCandidate(ctx, &v)
		if err != nil {
			return nil, err
		}
		if c.build == nil || c.build.Source != "staged" {
			continue
		}
		for _, cc := range c.build.Components {
			if d := imageDigest(cc.Image); d != "" && !slices.Contains(digests, d) {
				digests = append(digests, d)
			}
		}
	}
	return digests, nil
}

// candidateBuild returns the version's selected STAGE build, else its newest
// build, or nil when there is neither, with its images' ART builds, upstream
// commits and scans, and the STAGE build's stage Release's completion time.
func (s *Server) candidateBuild(ctx context.Context, release *model.ReleaseVersion, newest *model.ReleaseSnapshot) (*model.CandidateBuild, *time.Time, error) {
	staged, _, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		return nil, nil, err
	}
	snap, c := newest, &model.CandidateBuild{Source: "newest"}
	var stagedAt *time.Time
	if staged != nil {
		if snap, err = s.db.GetReleaseSnapshot(ctx, staged.SnapshotName); err != nil {
			return nil, nil, err
		}
		c.Source, stagedAt = "staged", &staged.CompletedAt
	}
	if snap == nil {
		return nil, nil, nil
	}
	c.Snapshot, c.CreatedAt = snap.Name, snap.CreatedAt
	images := buildImages(release.KonfluxApplication, snap)
	digests := make([]string, len(images))
	for i, img := range images {
		digests[i] = imageDigest(img.Image)
	}
	builds, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, nil, err
	}
	scans, err := s.db.ImageScans(ctx, digests)
	if err != nil {
		return nil, nil, err
	}
	c.Components = make([]model.CandidateComponent, len(images))
	for i, img := range images {
		c.Components[i] = model.CandidateComponent{Name: img.Name, Image: img.Image}
		if b, ok := builds[digests[i]]; ok {
			c.Components[i].NVR, c.Components[i].BuildURL = b.NVR, artbuild.PageURL(s.artBaseURL, b.NVR, b.RecordID)
			c.Components[i].UpstreamRepo, c.Components[i].UpstreamSHA = b.UpstreamRepo, b.UpstreamSHA
		}
		if sc, ok := scans[digests[i]]; ok {
			c.Components[i].Scan = &model.ImageScan{State: sc.State, Counts: sc.Counts, URL: sc.DetailURL}
		}
	}
	return c, stagedAt, nil
}

// stageState tells whether every image of a build is in a Snapshot a STAGE
// Release of the stream succeeded with: "staged" or "not_staged", or
// "unknown" when the stream has no STAGE Release at all: nothing shows it
// goes through stage.
func stageState(images []model.SnapshotImage, releases []model.StageRelease) string {
	if len(releases) == 0 {
		return "unknown"
	}
	staged := map[string]bool{}
	for _, r := range releases {
		if r.ReleasedStatus == "True" && r.ReleasedReason == "Succeeded" {
			for _, img := range r.Images {
				staged[imageDigest(img)] = true
			}
		}
	}
	for _, img := range images {
		if d := imageDigest(img.Image); d == "" || !staged[d] {
			return "not_staged"
		}
	}
	return "staged"
}

// ciJobs returns, per job, the newest of runs (newest first) that installed
// the operator bundle of digest bundle and how many did, sorted by job name.
func ciJobs(runs []prow.Run, bundle string) []model.CIJob {
	jobs := []model.CIJob{}
	for _, run := range runs {
		if !slices.Contains(run.Images, prow.Image{Role: "quay-operator-bundle", Digest: bundle}) {
			continue
		}
		i := slices.IndexFunc(jobs, func(j model.CIJob) bool { return j.JobName == run.JobName })
		if i < 0 {
			i = len(jobs)
			jobs = append(jobs, model.CIJob{JobName: run.JobName, State: run.State, ProwURL: run.ProwURL, StartedAt: run.StartedAt})
		}
		jobs[i].Runs++
	}
	slices.SortFunc(jobs, func(a, b model.CIJob) int { return strings.Compare(a.JobName, b.JobName) })
	return jobs
}

// buildImages returns snap's component images less the base image
// (<application>-base-*), which ART's assembly Snapshots leave out.
func buildImages(app string, snap *model.ReleaseSnapshot) []model.SnapshotImage {
	return slices.DeleteFunc(slices.Clone(snap.Components), func(c model.SnapshotImage) bool {
		return strings.HasPrefix(c.Name, app+"-base-")
	})
}

// imageDigest returns the digest of a repo@sha256:... image, or "" for a tag.
func imageDigest(image string) string {
	_, d, _ := strings.Cut(image, "@")
	return d
}
