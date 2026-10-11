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

// ciRoles are the images every periodic run deploys. A run tested a build
// only when it recorded the build's digest for all four: builds that share a
// bundle can differ in the others.
var ciRoles = []string{"quay", "clair", "quay-operator", "quay-operator-bundle"}

// handleGetCandidate serves what decides whether an unshipped version can
// ship: the build up for release and, per build that matters, whether it
// reached stage, the version's prod Release and the periodic jobs that tested
// it.
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
	resp := &model.ReleaseCandidate{Shipped: c.shipped, Candidate: c.build, Reason: c.reason, Builds: []model.BuildRow{}}
	if c.build == nil {
		return resp, nil
	}
	app := release.KonfluxApplication
	releases, err := s.db.StageReleases(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		return nil, err
	}
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return nil, err
	}
	row := func(snap *model.ReleaseSnapshot, roles ...string) model.BuildRow {
		images := buildImages(app, snap)
		b := model.BuildRow{Snapshot: snap.Name, CreatedAt: snap.CreatedAt, Roles: roles, Stage: stageFlag(app, images, releases), CI: []model.CIJob{}}
		var imgs []prow.Image
		for _, img := range images {
			role, d, _ := strings.Cut(prow.ComponentKey(app, img.Name, img.Image), "@")
			imgs = append(imgs, prow.Image{Role: role, Digest: d})
		}
		if build := ciBuild(imgs); build != nil {
			b.CI = ciJobs(runs, build)
		}
		return b
	}

	snap := &model.ReleaseSnapshot{Name: c.build.Snapshot, CreatedAt: c.build.CreatedAt}
	for _, cc := range c.build.Components {
		snap.Components = append(snap.Components, model.SnapshotImage{Name: cc.Name, Image: cc.Image})
	}
	roles := []string{"candidate"}
	if c.newest != nil && c.newest.Name == snap.Name {
		roles = append(roles, "newest")
	}
	cand := row(snap, roles...)
	if c.build.Source == "staged" && cand.Stage.State == "staged" {
		// ART assembles a STAGE build from images it often staged before,
		// so its own stage Release dates it.
		cand.Stage.StagedAt = c.stagedAt
	}
	if cand.Prod, err = s.db.LatestProdRelease(ctx, c.assembly); err != nil {
		return nil, err
	}
	resp.Builds = append(resp.Builds, cand)
	if c.newest != nil && c.newest.Name != snap.Name {
		resp.Builds = append(resp.Builds, row(c.newest, "newest"))
	}
	if slices.ContainsFunc(resp.Builds, func(b model.BuildRow) bool { return len(b.CI) > 0 }) {
		return resp, nil
	}
	if tested := lastTested(runs, c.builds); tested != nil {
		resp.Builds = append(resp.Builds, row(tested, "last_tested"))
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
	// build. stagedAt is the STAGE build's stage Release's completion time.
	// builds are its stream builds, newest first, as versionBuilds selects
	// them for its assembly.
	build    *model.CandidateBuild
	stagedAt *time.Time
	newest   *model.ReleaseSnapshot
	builds   []model.ReleaseSnapshot
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
	c := &candidate{builds: versionBuilds(app, builds, nvrs, m[1]), assembly: m[1]}
	if len(c.builds) > 0 {
		c.newest = &c.builds[0]
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

// stageFlag tells whether every image of a build is in a Snapshot a STAGE
// Release of the stream succeeded with and, when all are, dates it by the
// last image to get there: the latest over its images of the earliest such
// Release's completion. A stream with no STAGE Release at all is unknown:
// nothing shows it goes through stage. An image no STAGE Release holds waits
// on its bundle when the build's bundle for it is staged: ART builds an
// operator's bundle only after a clean build run.
func stageFlag(app string, images []model.SnapshotImage, releases []model.StageRelease) model.StageFlag {
	f := model.StageFlag{State: "unknown", Total: len(images), NotStaged: []string{}, NoBundle: []string{}, NoRelease: []string{}}
	if len(releases) == 0 {
		return f
	}
	staged := map[string]bool{}
	held := map[string]bool{}
	first := map[string]time.Time{}
	for _, r := range releases {
		for _, img := range r.Images {
			held[imageDigest(img)] = true
		}
		if r.ReleasedStatus == "True" && r.ReleasedReason == "Succeeded" {
			for _, img := range r.Images {
				d := imageDigest(img)
				staged[d] = true
				if t, ok := first[d]; r.CompletionTime != nil && (!ok || r.CompletionTime.Before(t)) {
					first[d] = *r.CompletionTime
				}
			}
		}
	}
	// bundled tells whether the build's bundle for a component, named as the
	// page names it with "-bundle" added, is staged.
	bundled := func(name string) bool {
		return slices.ContainsFunc(images, func(b model.SnapshotImage) bool {
			d := imageDigest(b.Image)
			return d != "" && staged[d] && componentLabel(app, b.Name) == componentLabel(app, name)+"-bundle"
		})
	}
	missing := map[string]bool{}
	var last time.Time
	for _, img := range images {
		d := imageDigest(img.Image)
		if d != "" && staged[d] {
			if first[d].After(last) {
				last = first[d]
			}
			continue
		}
		f.NotStaged = append(f.NotStaged, img.Name)
		if d != "" {
			missing[d] = true
		}
		switch {
		case d != "" && held[d]:
			// A STAGE Release that did not succeed holds it.
		case bundled(img.Name):
			f.NoBundle = append(f.NoBundle, img.Name)
		default:
			f.NoRelease = append(f.NoRelease, img.Name)
		}
	}
	if len(f.NotStaged) == 0 {
		f.State = "staged"
		if !last.IsZero() {
			f.StagedAt = &last
		}
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

// lastTested returns the newest of builds, newest first, that a periodic run
// tested: of the runs, newest first, that recorded all of ciRoles, the first
// whose build one of builds holds gives the newest such build. It is nil when
// none does.
func lastTested(runs []prow.Run, builds []model.ReleaseSnapshot) *model.ReleaseSnapshot {
	for _, run := range runs {
		build := ciBuild(run.Images)
		if build == nil {
			continue
		}
		for i, b := range builds {
			holds := true
			for _, d := range build {
				holds = holds && slices.ContainsFunc(b.Components, func(c model.SnapshotImage) bool { return imageDigest(c.Image) == d })
			}
			if holds {
				return &builds[i]
			}
		}
	}
	return nil
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

// componentLabel is a component's name as the page shows it, without its
// application and product: quay-3-18-quay-clair is clair.
func componentLabel(app, name string) string {
	return strings.TrimPrefix(strings.TrimPrefix(name, app+"-"), "quay-")
}

// imageDigest returns the digest of a repo@sha256:... image, or "" for a tag.
func imageDigest(image string) string {
	_, d, _ := strings.Cut(image, "@")
	return d
}
