package server

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

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
// ship: its release candidate, the STAGE build, with whether it reached stage
// and the version's prod Release, the periodic runs since its operator bundle
// was built, or its builds' without one, by the build each installed, and the
// version's newest build when it differs from the candidate or there is none.
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
	// Only the per-build stage Releases, of fbc-ri-* Snapshots, feed the
	// catalog CI installs from; the version's own stage Releases do not.
	releases = slices.DeleteFunc(releases, func(r model.StageRelease) bool { return !strings.HasPrefix(r.Snapshot, "fbc-ri-") })
	held := map[string]string{}
	if cand := c.build; cand != nil {
		resp.Candidate = cand
		for _, cc := range cand.Components {
			held[cc.Name] = imageDigest(cc.Image)
		}
		if cand.Prod, err = s.db.LatestProdRelease(ctx, c.assembly); err != nil {
			return nil, err
		}
	} else {
		resp.Reason = "no release candidate yet"
		if c.newest == nil {
			if c.art != "" && compareVersions(c.art, c.assembly) < 0 {
				resp.Reason = "no " + c.assembly + " builds yet: ART still builds " + c.art
			}
			return resp, nil
		}
	}
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return nil, err
	}
	catalogs := map[string][]string{}
	for _, run := range runs {
		if d := imageDigest(run.CatalogRef); run.InstallFailed && d != "" {
			if catalogs[d], err = s.db.FBCBundleNVRs(ctx, d); err != nil {
				return nil, err
			}
		}
	}
	var latest, passed string
	resp.CI, latest, passed = candidateCI(app, c, resp.Candidate, held, runs, releases, catalogs)
	if p := resp.CI.Passed; p != nil {
		if resp.CI.FBC, err = s.buildFBC(ctx, c, p.Snapshot); err != nil {
			return nil, err
		}
	}
	if c.newest == nil {
		return resp, nil
	}
	// The card names the build to pick or re-run with already, and beside the
	// candidate the build CI tests.
	bundle := bundleDigest(app, c.newest)
	if bundle != "" && (bundle == passed || (resp.Candidate != nil && bundle == latest)) {
		return resp, nil
	}
	// Only the components the candidate holds count: a stream builds images
	// its STAGE build can leave out.
	differs := slices.ContainsFunc(buildImages(app, c.newest), func(img model.SnapshotImage) bool {
		d, ok := held[img.Name]
		return ok && imageDigest(img.Image) != d
	})
	if resp.Candidate == nil || differs {
		resp.Newest = &model.NewestBuild{Snapshot: c.newest.Name, CreatedAt: c.newest.CreatedAt, CI: ciState(bundle, releases)}
	}
	return resp, nil
}

const (
	notInstalled = "not_installed"
	unknownBuild = "unknown"
)

// olmComponents is the component of each role a run's olm-csvs.yaml records.
var olmComponents = map[string]string{
	"quay-operator":     "quay-operator",
	"quay":              "quay-quay",
	"clair":             "quay-clair",
	"quay-builder":      "quay-builder",
	"quay-builder-qemu": "quay-builder-qemu",
}

// candidateCI groups the application's periodic runs since the candidate's
// operator bundle was first built, or without a candidate since the first of
// the version's builds' bundles was, by the build each installed; a run that
// installed no Quay counts only when it failed to install the build it tried.
// It finds the newest bundle of the version's builds in the CI catalog, when
// that is not the candidate's, and the newest, newer than the candidate, that
// a run passed with, and so whether to pick a candidate or re-run
// release-from-fbc; it also returns those two bundles. cand is the release
// candidate, or nil, held its image digests by component, and catalogs the
// bundle NVRs of ART's FBC records by digest.
func candidateCI(app string, c *candidate, cand *model.CandidateBuild, held map[string]string, runs []prow.Run, releases []model.StageRelease, catalogs map[string][]string) (ci *model.CandidateCI, latest, passed string) {
	// A bundle is labelled by the oldest stream build holding it: no run can
	// have installed it before.
	labels := map[string]model.BuildLabel{}
	for i := range c.stream {
		if d := bundleDigest(app, &c.stream[i]); d != "" {
			labels[d] = model.BuildLabel{Snapshot: c.stream[i].Name, CreatedAt: c.stream[i].CreatedAt}
		}
	}
	bundle := held[app+"-quay-operator-bundle"]
	ci = &model.CandidateCI{Builds: []model.CIBuild{}}
	if cand != nil {
		ci.Since = cand.CreatedAt
		if l, ok := labels[bundle]; ok {
			ci.Since = l.CreatedAt
		}
	} else {
		for i := range c.builds {
			if l, ok := labels[bundleDigest(app, &c.builds[i])]; ok && (ci.Since.IsZero() || l.CreatedAt.Before(ci.Since)) {
				ci.Since = l.CreatedAt
			}
		}
		if ci.Since.IsZero() {
			ci.Since = c.builds[len(c.builds)-1].CreatedAt
		}
	}

	var candRuns, unknown []prow.Run
	others := map[string][]prow.Run{}
	for _, run := range runs {
		if run.StartedAt == nil || run.StartedAt.Before(ci.Since) {
			continue
		}
		b := installedBundle(run, app, held)
		if b == notInstalled {
			if b = triedBundle(app, c, run, catalogs); b == "" {
				continue
			}
		}
		switch {
		case b == bundle && bundle != "":
			candRuns = append(candRuns, run)
		case labels[b].Snapshot != "":
			others[b] = append(others[b], run)
		default:
			unknown = append(unknown, run)
		}
	}
	if cand != nil {
		ci.Builds = append(ci.Builds, model.CIBuild{Kind: "candidate", Snapshot: cand.Snapshot, CreatedAt: &cand.CreatedAt, Jobs: jobRuns(candRuns)})
	}
	bundles := slices.Collect(maps.Keys(others))
	slices.SortFunc(bundles, func(a, b string) int {
		return cmp.Or(labels[b].CreatedAt.Compare(labels[a].CreatedAt), strings.Compare(labels[b].Snapshot, labels[a].Snapshot))
	})
	for _, b := range bundles {
		l, state := labels[b], ciState(b, releases)
		ci.Builds = append(ci.Builds, model.CIBuild{Kind: "build", Snapshot: l.Snapshot, CreatedAt: &l.CreatedAt, CI: &state, Jobs: jobRuns(others[b])})
	}
	if len(unknown) > 0 {
		ci.Builds = append(ci.Builds, model.CIBuild{Kind: unknownBuild, Jobs: jobRuns(unknown)})
	}

	// newestBundle returns, of the bundles of the version's builds that ok
	// accepts, the one with the newest label.
	newestBundle := func(ok func(d string) bool) string {
		newest := ""
		for i := range c.builds {
			d := bundleDigest(app, &c.builds[i])
			if d != "" && (newest == "" || labels[d].CreatedAt.After(labels[newest].CreatedAt)) && ok(d) {
				newest = d
			}
		}
		return newest
	}
	// The CI catalog serves the newest bundle of the version's builds that
	// reached it: a newer build can pair rebuilt images with an older bundle.
	latest = newestBundle(func(d string) bool { return ciState(d, releases).State == "in" })
	if latest == bundle {
		latest = ""
	}
	passed = newestBundle(func(d string) bool {
		return (cand == nil || labels[d].CreatedAt.After(ci.Since)) &&
			slices.ContainsFunc(others[d], func(r prow.Run) bool { return r.State == "success" })
	})
	if latest != "" {
		l := labels[latest]
		ci.LatestInCI = &l
	}
	if passed != "" {
		l := labels[passed]
		ci.Passed = &l
	}
	switch {
	case cand == nil && passed != "":
		ci.Refresh = "pick"
	case cand == nil:
		ci.Refresh = "none"
	case passed != "":
		ci.Refresh = "rerun"
	case ci.LatestInCI != nil && ci.LatestInCI.CreatedAt.After(ci.Since):
		ci.Refresh = "waiting"
	default:
		ci.Refresh = "current"
	}
	return ci, latest, passed
}

// installedBundle returns the operator bundle a run installed: the one it
// recorded, else the candidate's when it recorded the candidate's image of
// each role olm-csvs.yaml records, which runs that recorded no bundle did;
// unknownBuild for another Quay operator, and notInstalled without one.
func installedBundle(run prow.Run, app string, held map[string]string) string {
	for _, img := range run.Images {
		if img.Role == "quay-operator-bundle" {
			return img.Digest
		}
	}
	candidate := true
	for role, comp := range olmComponents {
		d := held[app+"-"+comp]
		candidate = candidate && d != "" && slices.Contains(run.Images, prow.Image{Role: role, Digest: d})
	}
	switch {
	case candidate:
		return held[app+"-quay-operator-bundle"]
	case slices.ContainsFunc(run.Images, func(img prow.Image) bool { return img.Role == "quay-operator" }):
		return unknownBuild
	}
	return notInstalled
}

// triedBundle returns the operator bundle a run that failed to install Quay
// tried: of the stream builds' bundles, the one whose ART NVR the FBC record
// of its catalog lists, by catalogs, or "" for any other run, for no FBC
// record, and for none or several bundles.
func triedBundle(app string, c *candidate, run prow.Run, catalogs map[string][]string) string {
	if !run.InstallFailed {
		return ""
	}
	nvrs, tried := catalogs[imageDigest(run.CatalogRef)], ""
	for i := range c.stream {
		for _, img := range c.stream[i].Components {
			if img.Name != app+"-quay-operator-bundle" || !slices.Contains(nvrs, c.nvrs[img.Image]) {
				continue
			}
			if d := imageDigest(img.Image); tried == "" {
				tried = d
			} else if d != tried {
				return ""
			}
		}
	}
	return tried
}

// jobRuns returns runs, newest first, by job, sorted by job name.
func jobRuns(runs []prow.Run) []model.CIJob {
	jobs := []model.CIJob{}
	for _, run := range runs {
		i := slices.IndexFunc(jobs, func(j model.CIJob) bool { return j.JobName == run.JobName })
		if i < 0 {
			i = len(jobs)
			jobs = append(jobs, model.CIJob{JobName: run.JobName})
		}
		jobs[i].Runs = append(jobs[i].Runs, model.CIRun{State: run.State, StartedAt: *run.StartedAt, ProwURL: run.ProwURL, InstallFailed: run.InstallFailed})
	}
	slices.SortFunc(jobs, func(a, b model.CIJob) int { return strings.Compare(a.JobName, b.JobName) })
	return jobs
}

// buildFBC returns the FBCs of the stream build named snapshot: of the FBC
// images of c's version for each operator and OCP version, the one ART built
// last that holds a bundle of the build, else the pair as missing. It is nil
// while the version has no FBC image or ART's record of one is unread.
func (s *Server) buildFBC(ctx context.Context, c *candidate, snapshot string) (*model.BuildFBC, error) {
	images, err := s.db.FBCImages(ctx, c.assembly)
	if err != nil || len(images) == 0 {
		return nil, err
	}
	// The ART NVRs of the build's images, of which only its bundles' can be
	// an FBC's.
	nvrs := map[string]bool{}
	if i := slices.IndexFunc(c.stream, func(b model.ReleaseSnapshot) bool { return b.Name == snapshot }); i >= 0 {
		for _, img := range c.stream[i].Components {
			if nvr, ok := c.nvrs[img.Image]; ok {
				nvrs[nvr] = true
			}
		}
	}
	type slot struct{ operator, ocp string }
	// Of each operator and OCP version's FBCs holding a bundle of the build,
	// the one ART built last: its build ended last, else it has the greater
	// NVR, then digest; nil for none.
	picks := map[slot]*artbuild.FBCImage{}
	for i := range images {
		img := &images[i]
		if img.Record == nil {
			return nil, nil
		}
		k := slot{img.Operator, img.OCP}
		p, ok := picks[k]
		if !ok {
			picks[k] = nil
		}
		if !slices.ContainsFunc(img.Record.BundleNVRs, func(n string) bool { return nvrs[n] }) {
			continue
		}
		if p == nil || cmp.Or(strings.Compare(img.Record.EndTime, p.Record.EndTime), strings.Compare(img.NVR, p.NVR),
			strings.Compare(img.Record.Digest, p.Record.Digest)) > 0 {
			picks[k] = img
		}
	}
	group := "quay-" + c.assembly[:strings.LastIndex(c.assembly, ".")]
	fbc := &model.BuildFBC{Pullspecs: []string{}, Missing: []model.MissingFBC{}}
	for _, k := range slices.SortedFunc(maps.Keys(picks), func(a, b slot) int {
		return cmp.Or(strings.Compare(a.operator, b.operator), compareVersions(a.ocp, b.ocp))
	}) {
		if p := picks[k]; p != nil {
			fbc.Pullspecs = append(fbc.Pullspecs, p.Record.Pullspec)
			continue
		}
		m := model.MissingFBC{Operator: k.operator, OCP: k.ocp}
		if s.artBaseURL != "" {
			m.URL = strings.TrimRight(s.artBaseURL, "/") + "/search?group=" + group + "&name=" + k.operator + "-fbc&assembly=stream"
		}
		fbc.Missing = append(fbc.Missing, m)
	}
	return fbc, nil
}

// ciState tells whether the operator bundle of digest bundle reached the
// catalog CI installs from, by releases, the per-build stage Releases, newest
// first.
func ciState(bundle string, releases []model.StageRelease) model.CIState {
	if bundle == "" || len(releases) == 0 {
		return model.CIState{State: "unknown"}
	}
	state := model.CIState{State: "none"}
	for _, r := range releases {
		if !slices.ContainsFunc(r.Images, func(img string) bool { return imageDigest(img) == bundle }) {
			continue
		}
		switch {
		case r.ReleasedStatus == "True" && r.ReleasedReason == "Succeeded":
			return model.CIState{State: "in", Release: &r.KonfluxRelease}
		case r.ReleasedReason == "Failed" && state.Release == nil:
			state = model.CIState{State: "failed", Release: &r.KonfluxRelease}
		}
	}
	return state
}

// bundleDigest returns the digest of a build's operator bundle, or "".
func bundleDigest(app string, snap *model.ReleaseSnapshot) string {
	for _, c := range snap.Components {
		if c.Name == app+"-quay-operator-bundle" {
			return imageDigest(c.Image)
		}
	}
	return ""
}

// candidate is a version's build up for release. A version that shipped, or
// that is not one ART builds, has no assembly and nothing more to decide.
type candidate struct {
	shipped bool
	// reason says why the version has no build.
	reason string
	// build is the version's selected STAGE build, or nil; newest its newest
	// build: the first of builds, its stream builds, newest first, as
	// versionBuilds selects them for its assembly from stream, all of the
	// application's.
	build          *model.CandidateBuild
	newest         *model.ReleaseSnapshot
	builds, stream []model.ReleaseSnapshot
	assembly       string
	// nvrs holds ART's resolved NVRs of the stream builds' images, by image.
	nvrs map[string]string
	// art is the version ART builds the stream at, that of its newest build
	// whose version resolves, or "".
	art string
}

// selectCandidate selects the version's build up for release.
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
	c := &candidate{assembly: m[1], builds: versionBuilds(app, builds, nvrs, m[1]), stream: builds, nvrs: nvrs}
	for i := 0; i < len(builds) && c.art == ""; i++ {
		c.art = buildVersion(app, &builds[i], nvrs)
	}
	if len(c.builds) > 0 {
		c.newest = &c.builds[0]
	}
	if c.build, err = s.candidateBuild(ctx, release); err != nil {
		return nil, err
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
		case v != "" && compareVersions(v, version) < 0:
			return out
		}
	}
	return out
}

// compareVersions compares dot-separated versions numerically: OCP 4.9 is
// lower than 4.12.
func compareVersions(a, b string) int {
	return slices.CompareFunc(strings.Split(a, "."), strings.Split(b, "."), func(x, y string) int {
		i, _ := strconv.Atoi(x)
		j, _ := strconv.Atoi(y)
		return cmp.Compare(i, j)
	})
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

// candidateBuild returns the version's selected STAGE build, or nil when it
// has none, with its images' ART builds.
func (s *Server) candidateBuild(ctx context.Context, release *model.ReleaseVersion) (*model.CandidateBuild, error) {
	staged, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil || staged == nil {
		return nil, err
	}
	snap, err := s.db.GetReleaseSnapshot(ctx, staged.SnapshotName)
	if err != nil {
		return nil, err
	}
	// Its own stage Release succeeded with every image of it, and dates it:
	// ART assembles a STAGE build from images it often staged before.
	c := &model.CandidateBuild{Snapshot: snap.Name, CreatedAt: snap.CreatedAt, Stage: model.StageFlag{StagedAt: &staged.CompletedAt}}
	images := buildImages(release.KonfluxApplication, snap)
	digests := make([]string, len(images))
	for i, img := range images {
		digests[i] = imageDigest(img.Image)
	}
	builds, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	c.Components = make([]model.CandidateComponent, len(images))
	for i, img := range images {
		c.Components[i] = model.CandidateComponent{Name: img.Name, Image: img.Image}
		if b, ok := builds[digests[i]]; ok {
			c.Components[i].NVR, c.Components[i].BuildURL = b.NVR, artbuild.PageURL(s.artBaseURL, b.NVR, b.RecordID)
		}
	}
	return c, nil
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
