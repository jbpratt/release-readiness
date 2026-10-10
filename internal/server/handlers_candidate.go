package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

// quayVersion is a concrete version, named after its ART assembly:
// quay-v3.18.1 is assembly 3.18.1.
var quayVersion = regexp.MustCompile(`^quay-v(\d+\.\d+\.\d+)$`)

// ciRoles are the images every periodic run deploys. A run tested a build
// only when it recorded the build's digest for all four: builds that share a
// bundle can differ in the others.
var ciRoles = []string{"quay", "clair", "quay-operator", "quay-operator-bundle"}

// handleGetCandidate serves what decides whether an unshipped version can
// ship: the build up for release, whether the stream's newest build reached
// stage, the version's prod Release and the periodic jobs that tested the
// build.
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
	resp := &model.ReleaseCandidate{Shipped: c.shipped, Candidate: c.build, Reason: c.reason, CI: model.CandidateCI{Jobs: []model.CIJob{}}}
	if c.assembly == "" {
		return resp, nil
	}
	app := release.KonfluxApplication
	if c.newest != nil {
		releases, err := s.db.StageReleases(ctx, release, s.StageReleasePlanPattern)
		if err != nil {
			return nil, err
		}
		resp.Stage = stageFlag(app, c.newest, releases)
	}
	if resp.Prod, err = s.db.LatestProdRelease(ctx, c.assembly); err != nil {
		return nil, err
	}
	if resp.CI, err = s.candidateCI(ctx, app, resp.Candidate); err != nil {
		return nil, err
	}
	return resp, nil
}

// candidate is a version's build up for release. A version that shipped, or
// that is not one ART builds, has no assembly and nothing more to decide.
type candidate struct {
	shipped bool
	// reason says why the version has no build.
	reason string
	// build is the version's selected STAGE build, else newest, the
	// stream's newest build.
	build    *model.CandidateBuild
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
	newest, err := s.db.NewestStreamBuild(ctx, app)
	if err != nil {
		return nil, err
	}
	c := &candidate{newest: newest, assembly: m[1]}
	if c.build, err = s.candidateBuild(ctx, release, newest); err != nil {
		return nil, err
	}
	if c.build == nil {
		c.reason = "no build of " + app
	}
	return c, nil
}

// CandidateDigests returns the image digests of the build up for release of
// every version that is not archived; released versions select none.
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
		if c.build == nil {
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

// candidateBuild returns the version's selected STAGE build, else the
// stream's newest build, or nil when there is neither, with its images' ART
// builds and scans.
func (s *Server) candidateBuild(ctx context.Context, release *model.ReleaseVersion, newest *model.ReleaseSnapshot) (*model.CandidateBuild, error) {
	staged, _, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		return nil, err
	}
	snap, c := newest, &model.CandidateBuild{Source: "newest"}
	if staged != nil {
		if snap, err = s.db.GetReleaseSnapshot(ctx, staged.SnapshotName); err != nil {
			return nil, err
		}
		c.Source, c.StagedAt = "staged", &staged.CompletedAt
	}
	if snap == nil {
		return nil, nil
	}
	c.Snapshot, c.CreatedAt = snap.Name, snap.CreatedAt
	images := buildImages(release.KonfluxApplication, snap)
	digests := make([]string, len(images))
	for i, img := range images {
		digests[i] = imageDigest(img.Image)
	}
	builds, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	scans, err := s.db.ImageScans(ctx, digests)
	if err != nil {
		return nil, err
	}
	c.Components = make([]model.CandidateComponent, len(images))
	for i, img := range images {
		c.Components[i] = model.CandidateComponent{Name: img.Name, Image: img.Image}
		if b, ok := builds[digests[i]]; ok {
			c.Components[i].NVR, c.Components[i].BuildURL = b.NVR, artbuild.PageURL(s.artBaseURL, b.NVR, b.RecordID)
		}
		if sc, ok := scans[digests[i]]; ok {
			c.Components[i].Scan = &model.ImageScan{State: sc.State, Counts: sc.Counts, URL: sc.DetailURL}
		}
	}
	return c, nil
}

// stageFlag tells whether every image of the stream's newest build is in a
// Snapshot a STAGE Release of the stream succeeded with. A stream with no
// STAGE Release at all is unknown: nothing shows it goes through stage.
func stageFlag(app string, newest *model.ReleaseSnapshot, releases []model.StageRelease) *model.StageFlag {
	images := buildImages(app, newest)
	f := &model.StageFlag{State: "unknown", Snapshot: newest.Name, CreatedAt: newest.CreatedAt, Total: len(images), NotStaged: []string{}}
	if len(releases) == 0 {
		return f
	}
	staged := map[string]bool{}
	for _, r := range releases {
		if r.ReleasedStatus == "True" && r.ReleasedReason == "Succeeded" {
			for _, img := range r.Images {
				staged[imageDigest(img)] = true
			}
		}
	}
	missing := map[string]bool{}
	for _, img := range images {
		d := imageDigest(img.Image)
		if d != "" && staged[d] {
			continue
		}
		f.NotStaged = append(f.NotStaged, img.Name)
		if d != "" {
			missing[d] = true
		}
	}
	if len(f.NotStaged) == 0 {
		f.State = "staged"
		return f
	}
	f.State = "not_staged"
	// A Release holding a missing digest cannot have succeeded.
	for _, r := range releases {
		if slices.ContainsFunc(r.Images, func(img string) bool { return missing[imageDigest(img)] }) {
			f.Release = &r.KonfluxRelease
			break
		}
	}
	return f
}

// candidateCI lists the periodic jobs whose runs tested the candidate or, when
// none did, the newest build a run recorded all of ciRoles for.
func (s *Server) candidateCI(ctx context.Context, app string, c *model.CandidateBuild) (model.CandidateCI, error) {
	ci := model.CandidateCI{Jobs: []model.CIJob{}}
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return ci, err
	}
	if c != nil {
		var imgs []prow.Image
		for _, cc := range c.Components {
			role, d, _ := strings.Cut(prow.ComponentKey(app, cc.Name, cc.Image), "@")
			imgs = append(imgs, prow.Image{Role: role, Digest: d})
		}
		if build := ciBuild(imgs); build != nil {
			ci.Jobs = ciJobs(runs, build)
		}
	}
	if len(ci.Jobs) > 0 {
		return ci, nil
	}
	for _, run := range runs {
		build := ciBuild(run.Images)
		if build == nil {
			continue
		}
		bundle := build["quay-operator-bundle"]
		art, err := s.db.ResolvedArtBuilds(ctx, []string{bundle})
		if err != nil {
			return ci, err
		}
		ci.LastTested = &model.LastTested{BundleNVR: art[bundle].NVR, TestedAt: run.StartedAt, Jobs: ciJobs(runs, build)}
		break
	}
	return ci, nil
}

// ciBuild returns the first digest imgs hold for each of ciRoles, or nil when
// one is missing.
func ciBuild(imgs []prow.Image) map[string]string {
	build := map[string]string{}
	for _, img := range imgs {
		if _, ok := build[img.Role]; !ok && img.Digest != "" && slices.Contains(ciRoles, img.Role) {
			build[img.Role] = img.Digest
		}
	}
	if len(build) < len(ciRoles) {
		return nil
	}
	return build
}

// ciJobs returns, per job, the newest of runs (newest first) that recorded
// every digest of build and how many did, sorted by job name.
func ciJobs(runs []prow.Run, build map[string]string) []model.CIJob {
	jobs := []model.CIJob{}
	for _, run := range runs {
		tested := true
		for role, d := range build {
			tested = tested && slices.Contains(run.Images, prow.Image{Role: role, Digest: d})
		}
		if !tested {
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
