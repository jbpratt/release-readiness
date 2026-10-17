package release

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

// nvrXYZ is the X.Y.Z an NVR's version field starts with, which ends at a dot
// or at the field's end: 3.18.10 is not 3.18.1.
var nvrXYZ = regexp.MustCompile(`^(\d+\.\d+\.\d+)(?:\.|$)`)

// ReleaseCandidate is the version's candidate response, with its verdict, and
// the Prow runs of its application it read. A shipped version has no reason.
func (s *Selector) ReleaseCandidate(ctx context.Context, release *model.ReleaseVersion) (*model.ReleaseCandidate, []prow.Run, error) {
	c, err := s.SelectCandidate(ctx, release)
	if err != nil {
		return nil, nil, err
	}
	resp, runs, err := s.CandidateResponse(ctx, release, c)
	if err != nil {
		return nil, nil, err
	}
	// A shipped version's record is its candidate alone.
	shown := []*model.FBCCandidate{resp.FBCCandidate}
	if c.shipped {
		resp.Reason = ""
	} else {
		shown = append(shown, resp.FBCStream)
	}
	r := &resp.Reading
	if r.Read, r.Failed, r.Pending, err = s.db.FBCReadProgress(ctx, FBCDigests(shown...)); err != nil {
		return nil, nil, err
	}
	_, _, pending, err := s.db.FBCReadProgress(ctx, FBCDigests(resp.FBCCandidate))
	if err != nil {
		return nil, nil, err
	}
	if !c.shipped && c.Assembly != "" && s.ArtBaseURL != "" {
		group := "quay-" + c.Assembly[:strings.LastIndex(c.Assembly, ".")]
		scanned, err := s.db.ScannedArtHistoryDays(ctx, group, artbuild.TypeFBC)
		if err != nil {
			return nil, nil, err
		}
		r.HistoryDays = artbuild.UnscannedDays(time.Now(), scanned)
	}
	setVerdict(resp, c.Assembly, pending)
	return resp, runs, nil
}

// FBCDigests returns the digests of the FBC images of cs, the ones the page
// shows.
func FBCDigests(cs ...*model.FBCCandidate) []string {
	var digests []string
	for _, c := range cs {
		if c == nil {
			continue
		}
		for _, o := range c.Operators {
			for _, m := range slices.Concat(o.Members, o.Conflicts) {
				digests = append(digests, m.Digest)
			}
		}
		for _, m := range c.Unknown {
			digests = append(digests, m.Digest)
		}
	}
	return digests
}

func (s *Selector) CandidateResponse(ctx context.Context, release *model.ReleaseVersion, c *Candidate) (*model.ReleaseCandidate, []prow.Run, error) {
	var err error
	resp := &model.ReleaseCandidate{Shipped: c.shipped, Reason: c.Reason}
	if c.Assembly == "" {
		return resp, nil, nil
	}
	app := release.KonfluxApplication
	releases, err := s.db.StageReleases(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		return nil, nil, err
	}
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return nil, nil, err
	}
	if resp.FBCCandidate, resp.FBCStream, resp.CIHistory, err = s.fbcCandidates(ctx, app, c, releases, runs); err != nil {
		return nil, nil, err
	}
	if resp.Catalogs, err = s.candidateCatalogs(ctx, app, c.Assembly); err != nil {
		return nil, nil, err
	}
	cand := c.build
	if cand == nil {
		resp.Reason = "no release candidate yet"
		if c.newest == nil && c.art != "" && CompareVersions(c.art, c.Assembly) < 0 {
			resp.Reason = "no " + c.Assembly + " builds yet: ART still builds " + c.art
		}
		return resp, runs, nil
	}
	resp.Candidate = cand
	held := map[string]string{}
	for _, cc := range cand.Components {
		held[cc.Name] = ImageDigest(cc.Image)
	}
	if resp.UnshippedBuilds, err = s.unshippedBuilds(ctx, app, c, held); err != nil {
		return nil, nil, err
	}
	if cand.Prod, err = s.db.LatestProdRelease(ctx, c.Assembly); err != nil {
		return nil, nil, err
	}
	return resp, runs, nil
}

// unshippedBuilds returns, sorted by component, the newest image of the
// version of each component that held, the candidate's images by component,
// leaves out. A STAGE build ships only its shipment's components; ART stages
// the others another way.
func (s *Selector) unshippedBuilds(ctx context.Context, app string, c *Candidate, held map[string]string) ([]model.UnshippedBuild, error) {
	var picked model.ReleaseSnapshot
	var snapshots []string
	for i := range c.builds {
		for _, img := range buildImages(app, &c.builds[i]) {
			_, ok := held[img.Name]
			if ok || nvrVersion(c.nvrs[img.Image]) != c.Assembly ||
				slices.ContainsFunc(picked.Components, func(p model.SnapshotImage) bool { return p.Name == img.Name }) {
				continue
			}
			picked.Components = append(picked.Components, img)
			snapshots = append(snapshots, c.builds[i].Name)
		}
	}
	components, err := s.buildComponents(ctx, app, &picked)
	if err != nil {
		return nil, err
	}
	out := make([]model.UnshippedBuild, len(components))
	for i, cc := range components {
		out[i] = model.UnshippedBuild{CandidateComponent: cc, Snapshot: snapshots[i]}
	}
	slices.SortFunc(out, func(a, b model.UnshippedBuild) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
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

// candidateCatalogs returns the FBC catalogs of assembly: those of the FBC
// Snapshots of app ART staged for stage, then, by operator and OCP version,
// the newest catalog of the FBC stream ART built for assembly, unless it
// staged that catalog.
func (s *Selector) candidateCatalogs(ctx context.Context, app, assembly string) ([]model.CandidateCatalog, error) {
	fbcApp := "fbc-" + app
	catalogs, err := s.db.StagedFBCCatalogs(ctx, assembly, fbcApp)
	if err != nil {
		return nil, err
	}
	n, staged := len(catalogs), map[string]bool{}
	for _, c := range catalogs {
		staged[ImageDigest(c.Pullspec)] = true
	}
	builds, nvrs, err := s.db.StreamBuilds(ctx, fbcApp)
	if err != nil {
		return nil, err
	}
	type slot struct{ operator, ocp string }
	// newest holds the index in catalogs of each slot's newest catalog, -1
	// when that one is staged.
	newest := map[slot]int{}
	for i := range builds {
		for _, img := range builds[i].Components {
			nvr := nvrs[img.Image]
			operator, version, ocp, ok := artbuild.ParseFBC(nvr)
			if !ok || version != assembly {
				continue
			}
			k := slot{operator, ocp}
			j, seen := newest[k]
			switch {
			case seen && j >= 0 && catalogs[j].Pullspec == img.Image:
				// builds is newest first.
				catalogs[j].Snapshot = builds[i].Name
			case seen:
			case staged[ImageDigest(img.Image)]:
				newest[k] = -1
			default:
				newest[k] = len(catalogs)
				catalogs = append(catalogs, model.CandidateCatalog{State: "built", Snapshot: builds[i].Name, Component: img.Name, Pullspec: img.Image, NVR: nvr, OCP: ocp})
			}
		}
	}
	slices.SortFunc(catalogs[n:], func(a, b model.CandidateCatalog) int {
		return cmp.Or(strings.Compare(a.Component, b.Component), CompareVersions(a.OCP, b.OCP))
	})
	return catalogs, nil
}

// fbcCandidates returns c's operator FBCs up for release, as ART staged them
// for stage in app's FBC Snapshots, else as its stream built them, and as its
// stream built them; nil while it has no FBC built or staged. Each slot's FBC
// carries the runs that installed from it or from equivalent content, and
// each operator whether releases, the stage Releases, staged its head
// bundle, how that bundle compares with the image Snapshot's, and its images.
func (s *Selector) fbcCandidates(ctx context.Context, app string, c *Candidate, releases []model.StageRelease, runs []prow.Run) (cand, stream *model.FBCCandidate, history []model.CISlot, err error) {
	assembly := c.Assembly
	built, err := s.db.ArtHistoryFBCs(ctx, "quay-"+assembly[:strings.LastIndex(assembly, ".")])
	if err != nil {
		return nil, nil, nil, err
	}
	// The group holds every z of the stream.
	built = slices.DeleteFunc(built, func(m model.FBCMember) bool {
		_, version, _, _ := artbuild.ParseFBC(m.NVR)
		return version != assembly
	})
	staged, err := s.db.StagedFBCMembers(ctx, assembly, "fbc-"+app)
	if err != nil || len(built) == 0 && len(staged) == 0 {
		return nil, nil, nil, err
	}
	// An operator's slots are the OCP versions any of its FBCs of the version
	// was built or staged for: no other can be inferred.
	slots := map[string][]string{}
	type content struct{ operator, ocp, head string }
	contents := map[string]content{}
	for _, rows := range [][]model.FBCMember{built, staged} {
		for i := range rows {
			if rows[i].RecordID != "" {
				rows[i].BuildURL = artbuild.PageURL(s.ArtBaseURL, rows[i].NVR, rows[i].RecordID)
			}
			operator, _, ocp, ok := artbuild.ParseFBC(rows[i].NVR)
			if !ok {
				continue
			}
			if !slices.Contains(slots[operator], ocp) {
				slots[operator] = append(slots[operator], ocp)
			}
			contents[rows[i].Digest] = content{operator, ocp, rows[i].HeadBundleNVR}
		}
	}
	for _, ocps := range slots {
		slices.SortFunc(ocps, CompareVersions)
	}
	stream = projectFBC("stream", built, slots)
	cand = stream
	if len(staged) > 0 {
		cand = projectFBC("stage_snapshots", staged, slots)
		// staged is one row per Snapshot and catalog, so a slot restaged in
		// several Snapshots counts each, and a superseded failure none.
		times := map[[2]string]int{}
		// A digest is on stage once any Release of a Snapshot holding it
		// succeeded, else failed once any failed. Its member shows the newest
		// such Snapshot and Release, so a failed restage of a digest already
		// on stage reads as on stage.
		ok, failed := map[string]model.FBCMember{}, map[string]model.FBCMember{}
		for _, m := range staged {
			switch {
			case succeeded(m.Release):
				operator, _, ocp, _ := artbuild.ParseFBC(m.NVR)
				times[[2]string{operator, ocp}]++
				if _, seen := ok[m.Digest]; !seen {
					ok[m.Digest] = m
				}
			case m.Release != nil && m.Release.ReleasedReason == "Failed":
				if _, seen := failed[m.Digest]; !seen {
					failed[m.Digest] = m
				}
			}
		}
		for i := range cand.Operators {
			o := &cand.Operators[i]
			for _, ms := range [][]model.FBCMember{o.Members, o.Conflicts} {
				for j := range ms {
					ms[j].Staged = times[[2]string{o.Operator, ms[j].OCP}]
					if r, seen := ok[ms[j].Digest]; seen {
						ms[j].Snapshot, ms[j].Release = r.Snapshot, r.Release
					} else if r, seen := failed[ms[j].Digest]; seen {
						ms[j].Snapshot, ms[j].Release, ms[j].StageFailed = r.Snapshot, r.Release, r.Release
					}
				}
			}
		}
	}

	var digests []string
	for _, r := range releases {
		for _, img := range r.Images {
			digests = append(digests, ImageDigest(img))
		}
	}
	resolved, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, nil, nil, err
	}
	// The digest of each bundle releases staged, by NVR.
	staging := map[string]string{}
	for d, b := range resolved {
		staging[b.NVR] = d
	}
	image, bundles := "", map[string]string{}
	switch {
	case c.build != nil:
		image = c.build.Snapshot
		for _, cc := range c.build.Components {
			bundles[cc.Name] = cc.NVR
		}
	case c.newest != nil:
		image = c.newest.Name
		for _, img := range c.newest.Components {
			bundles[img.Name] = c.nvrs[img.Image]
		}
	}
	// ci returns the runs that installed from m, of operator, or from another
	// FBC of its slot and head bundle.
	ci := func(operator string, m model.FBCMember) *model.FBCCI {
		var exact, equivalent []prow.Run
		for _, run := range runs {
			d := ImageDigest(run.CatalogRef)
			switch {
			case run.StartedAt == nil || d == "":
			case d == m.Digest:
				exact = append(exact, run)
			case m.HeadBundleNVR != "" && contents[d] == (content{operator, m.OCP, m.HeadBundleNVR}):
				equivalent = append(equivalent, run)
			}
		}
		return &model.FBCCI{Exact: jobRuns(exact), Equivalent: jobRuns(equivalent)}
	}
	for _, f := range []*model.FBCCandidate{cand, stream} {
		for i := range f.Operators {
			o := &f.Operators[i]
			for _, ms := range [][]model.FBCMember{o.Members, o.Conflicts} {
				for j := range ms {
					ms[j].CI = ci(o.Operator, ms[j])
				}
			}
			o.FBCRIStage = model.CIState{State: "unknown"}
			if o.HeadBundleNVR != "" {
				o.FBCRIStage = model.CIState{State: "none"}
				if d, ok := staging[o.HeadBundleNVR]; ok {
					o.FBCRIStage = ciState(d, releases)
				}
			}
			o.Stage = model.StageSummary{Total: len(o.Members)}
			for _, m := range o.Members {
				if succeeded(m.Release) {
					o.Stage.Staged++
				}
			}
			o.ImageCheck = model.ImageCheck{State: "absent", Snapshot: image, BundleNVR: bundles[app+"-"+o.Operator+"-bundle"]}
			// The image shipment can hold the bundle under another component
			// name, so it is found by its NVR's name, whatever its version.
			for _, nvr := range bundles {
				if o.ImageCheck.BundleNVR == o.HeadBundleNVR {
					break
				}
				if name := nvrName(nvr); name != "" && name == nvrName(o.HeadBundleNVR) {
					o.ImageCheck.BundleNVR = nvr
				}
			}
			if o.ImageCheck.BundleNVR != "" && o.HeadBundleNVR != "" {
				o.ImageCheck.State = "differs"
				if o.ImageCheck.BundleNVR == o.HeadBundleNVR {
					o.ImageCheck.State = "matches"
				}
			}
		}
	}
	history = ciHistory(slots, cand)

	shipped, err := s.db.ProdReleasedImages(ctx, app)
	if err != nil {
		return nil, nil, nil, err
	}
	prod := map[string]bool{}
	for _, img := range shipped {
		prod[ImageDigest(img)] = true
	}
	fs := []*model.FBCCandidate{cand}
	if stream != cand {
		fs = append(fs, stream)
	}
	for _, f := range fs {
		for i := range f.Operators {
			if err := s.operatorImages(ctx, &f.Operators[i], releases, prod); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	if stream != cand {
		for i := range cand.Operators {
			o, so := &cand.Operators[i], &stream.Operators[i]
			if o.HeadBundleNVR != "" && so.HeadBundleNVR != "" && so.HeadBundleNVR != o.HeadBundleNVR && len(o.Images) > 0 && len(so.Images) > 0 {
				o.Compare = append(o.Compare, model.ImageCompare{Kind: "newer", Label: so.HeadBundleNVR, Rows: compareImages(o.Images, so.Images)})
			}
		}
	}
	return cand, stream, history, nil
}

// ciHistory returns, by cand's operators and their slots, the CI of cand's
// FBC of the slot, nil without one.
func ciHistory(slots map[string][]string, cand *model.FBCCandidate) []model.CISlot {
	out := []model.CISlot{}
	for _, o := range cand.Operators {
		ms := slices.Concat(o.Members, o.Conflicts)
		for _, ocp := range slots[o.Operator] {
			slot := model.CISlot{Operator: o.Operator, OCP: ocp}
			if i := slices.IndexFunc(ms, func(m model.FBCMember) bool { return m.OCP == ocp }); i >= 0 {
				slot.CI = ms[i].CI
			}
			out = append(out, slot)
		}
	}
	return out
}

// operatorImages sets o's Images, the head bundle's relatedImages of its
// newest-OCP member FBC with a render, each with its ART build, its build page
// (its resolved build's, else its ART history record's), and whether a stage
// Release of releases or a prod Release, by prod digests, shipped it; and its
// Compare with that FBC's previous entry.
func (s *Selector) operatorImages(ctx context.Context, o *model.OperatorFBC, releases []model.StageRelease, prod map[string]bool) error {
	o.Images, o.Compare = []model.OperatorImage{}, []model.ImageCompare{}
	var head, previous []fbc.RelatedImage
	var m model.FBCMember
	for i := len(o.Members) - 1; i >= 0 && len(head) == 0; i-- {
		m = o.Members[i]
		var err error
		if head, previous, err = s.db.FBCRelatedImages(ctx, m.Digest); err != nil {
			return err
		}
	}
	var digests []string
	for _, img := range slices.Concat(head, previous) {
		digests = append(digests, ImageDigest(img.Image))
	}
	builds, err := s.db.ArtHistoryImages(ctx, digests)
	if err != nil {
		return err
	}
	resolved, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return err
	}
	scans, err := s.db.ImageScans(ctx, digests)
	if err != nil {
		return err
	}
	images := func(related []fbc.RelatedImage) []model.OperatorImage {
		out := []model.OperatorImage{}
		for _, img := range related {
			d := ImageDigest(img.Image)
			b := builds[d]
			oi := model.OperatorImage{
				Name: img.Name, Image: img.Image, Digest: d, NVR: b.NVR, Commit: b.Commitish,
				Stage: ciState(d, releases).State == "in", Prod: prod[d],
			}
			if r, ok := resolved[d]; ok {
				oi.BuildURL = artbuild.PageURL(s.ArtBaseURL, r.NVR, r.RecordID)
			} else if b.RecordID != "" {
				oi.BuildURL = artbuild.PageURL(s.ArtBaseURL, b.NVR, b.RecordID)
			}
			if sc, ok := scans[d]; ok {
				oi.Scan = &model.ImageScan{State: sc.State, Counts: sc.Counts, URL: sc.DetailURL}
			}
			out = append(out, oi)
		}
		return out
	}
	o.Images = images(head)
	if len(previous) > 0 {
		o.Compare = append(o.Compare, model.ImageCompare{Kind: "previous", Label: m.PreviousVersion, Rows: compareImages(images(previous), o.Images)})
	}
	return nil
}

// compareImages pairs from and to by repository where each side has one
// image of it, else by name: ART renames relatedImages between bundles. A
// pair by name across repositories is "renamed". Rows are to's in its order,
// as to names them, then those only from has.
func compareImages(from, to []model.OperatorImage) []model.ImageCompareRow {
	ref := func(img model.OperatorImage) *model.ImageRef {
		return &model.ImageRef{Repository: repository(img.Image), NVR: img.NVR, Commit: img.Commit, Digest: img.Digest, BuildURL: img.BuildURL}
	}
	count := func(imgs []model.OperatorImage, repo string) int {
		n := 0
		for _, img := range imgs {
			if repository(img.Image) == repo {
				n++
			}
		}
		return n
	}
	pair, paired := make([]int, len(to)), make([]bool, len(from))
	for i, t := range to {
		pair[i] = -1
		if r := repository(t.Image); count(to, r) == 1 && count(from, r) == 1 {
			j := slices.IndexFunc(from, func(f model.OperatorImage) bool { return repository(f.Image) == r })
			pair[i], paired[j] = j, true
		}
	}
	for i, t := range to {
		for j, f := range from {
			if pair[i] < 0 && !paired[j] && f.Name == t.Name {
				pair[i], paired[j] = j, true
			}
		}
	}
	rows := []model.ImageCompareRow{}
	for i, t := range to {
		row := model.ImageCompareRow{Name: t.Name, To: ref(t), Change: "added"}
		if j := pair[i]; j >= 0 {
			f := from[j]
			row.From = ref(f)
			switch {
			case row.From.Repository != row.To.Repository:
				row.Change = "renamed"
			case f.Digest == t.Digest:
				row.Change = "same"
			case f.Commit == "" || t.Commit == "":
				row.Change = "new build"
			case f.Commit == t.Commit:
				row.Change = "rebuild"
			default:
				row.Change = "new source"
			}
		}
		rows = append(rows, row)
	}
	for j, f := range from {
		if !paired[j] {
			rows = append(rows, model.ImageCompareRow{Name: f.Name, From: ref(f), Change: "removed"})
		}
	}
	return rows
}

// repository returns image without its digest or tag.
func repository(image string) string {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		image = image[:i]
	}
	return image
}

// projectFBC returns the operator FBCs that rows, newest first, give each of
// slots, the OCP versions of each operator: the newest row of each slot, never
// several. Rows no NVR names fill no slot.
func projectFBC(source string, rows []model.FBCMember, slots map[string][]string) *model.FBCCandidate {
	c := &model.FBCCandidate{Source: source, Errors: []string{}, Operators: []model.OperatorFBC{}, Unknown: []model.FBCMember{}}
	type slot struct{ operator, ocp string }
	filled := map[slot]bool{}
	picks := map[string][]model.FBCMember{}
	for _, m := range rows {
		operator, _, ocp, ok := artbuild.ParseFBC(m.NVR)
		switch {
		case !ok:
			if !slices.ContainsFunc(c.Unknown, func(u model.FBCMember) bool { return u.Digest == m.Digest }) {
				c.Unknown = append(c.Unknown, m)
			}
		case !filled[slot{operator, ocp}]:
			filled[slot{operator, ocp}] = true
			m.OCP = ocp
			picks[operator] = append(picks[operator], m)
		}
	}
	mixed, partial, unknown := false, false, len(c.Unknown) > 0
	byOCP := func(a, b model.FBCMember) int { return CompareVersions(a.OCP, b.OCP) }
	for _, operator := range artbuild.Operators {
		o := model.OperatorFBC{Operator: operator, Members: []model.FBCMember{}, MissingOCP: []string{}, Conflicts: []model.FBCMember{}}
		p := picks[operator]
		if i := slices.IndexFunc(p, func(m model.FBCMember) bool { return m.HeadBundleNVR != "" }); i >= 0 {
			o.HeadBundleNVR, o.Channel, o.HeadCSV = p[i].HeadBundleNVR, p[i].Channel, p[i].HeadCSV
		}
		other := false
		for _, m := range p {
			if o.HeadBundleNVR != "" && m.HeadBundleNVR == o.HeadBundleNVR {
				o.Members = append(o.Members, m)
				continue
			}
			o.Conflicts = append(o.Conflicts, m)
			other, unknown = other || m.HeadBundleNVR != "", unknown || m.HeadBundleNVR == ""
		}
		mixed = mixed || other
		slices.SortFunc(o.Members, byOCP)
		slices.SortFunc(o.Conflicts, byOCP)
		for _, ocp := range slots[operator] {
			if !filled[slot{operator, ocp}] {
				o.MissingOCP = append(o.MissingOCP, ocp)
				partial = true
			}
		}
		o.Coverage = model.FBCCoverage{Members: len(o.Members), Slots: len(slots[operator])}
		switch {
		case len(p) == 0 && source == "stream":
			c.Errors, partial = append(c.Errors, operator+" has no FBC build"), true
		case len(p) == 0:
			c.Errors, partial = append(c.Errors, operator+" is not in stage"), true
		case other:
			c.Errors = append(c.Errors, operator+" heads more than one bundle across its OCP versions")
		}
		c.Operators = append(c.Operators, o)
	}
	switch {
	case mixed:
		c.State = "inconsistent"
	case partial:
		c.State = "incomplete"
	case unknown:
		c.State = "unknown"
	default:
		c.State = "complete"
	}
	// A partial or mixed shipment must not be released.
	c.Pullspecs = []string{}
	if c.State == "complete" {
		for _, o := range c.Operators {
			for _, m := range o.Members {
				c.Pullspecs = append(c.Pullspecs, m.Pullspec)
			}
		}
	}
	return c
}

// ciState tells whether the operator bundle of digest bundle reached the
// catalog CI installs from, by releases, the stage Releases, newest first.
func ciState(bundle string, releases []model.StageRelease) model.CIState {
	if bundle == "" || len(releases) == 0 {
		return model.CIState{State: "unknown"}
	}
	state := model.CIState{State: "none"}
	for _, r := range releases {
		if !slices.ContainsFunc(r.Images, func(img string) bool { return ImageDigest(img) == bundle }) {
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

func succeeded(r *model.KonfluxRelease) bool {
	return r != nil && r.ReleasedStatus == "True" && r.ReleasedReason == "Succeeded"
}

// Candidate is a version's build up for release. A version that is not one
// ART builds has no assembly and nothing more to decide.
type Candidate struct {
	shipped bool
	// Reason says why the version has no build.
	Reason string
	// build is the version's selected STAGE build, or nil; newest its newest
	// build: the first of builds, its stream builds, newest first, as
	// VersionBuilds selects them for its assembly from all of the
	// application's.
	build    *model.CandidateBuild
	newest   *model.ReleaseSnapshot
	builds   []model.ReleaseSnapshot
	Assembly string
	// nvrs holds ART's resolved NVRs of the stream builds' images, by image.
	nvrs map[string]string
	// art is the version ART builds the stream at, that of its newest build
	// whose version resolves, or "".
	art string
}

// SelectCandidate selects the version's build up for release.
func (s *Selector) SelectCandidate(ctx context.Context, release *model.ReleaseVersion) (*Candidate, error) {
	app := release.KonfluxApplication
	m := quayVersion.FindStringSubmatch(release.Name)
	shipped := release.Released || s.CatalogShipped(release.Name)
	switch {
	case app == "":
		return &Candidate{shipped: shipped, Reason: "no Konflux application"}, nil
	case m == nil:
		return &Candidate{shipped: shipped, Reason: "not a concrete quay-vX.Y.Z version"}, nil
	}
	builds, nvrs, err := s.db.StreamBuilds(ctx, app)
	if err != nil {
		return nil, err
	}
	c := &Candidate{shipped: shipped, Assembly: m[1], builds: VersionBuilds(app, builds, nvrs, m[1]), nvrs: nvrs}
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

// VersionBuilds returns, of an application's stream builds, newest first,
// those of version: whose buildVersion, that of its operator bundles when ART
// resolved any, is version and after which ART built no lower version. ART can
// go back to a lower version; the builds before that were premature.
func VersionBuilds(app string, builds []model.ReleaseSnapshot, nvrs map[string]string, version string) []model.ReleaseSnapshot {
	var out []model.ReleaseSnapshot
	for i := range builds {
		switch v := buildVersion(app, &builds[i], nvrs); {
		case v == version:
			out = append(out, builds[i])
		case v != "" && CompareVersions(v, version) < 0:
			return out
		}
	}
	return out
}

// CompareVersions compares dot-separated versions numerically: OCP 4.9 is
// lower than 4.12.
func CompareVersions(a, b string) int {
	return slices.CompareFunc(strings.Split(a, "."), strings.Split(b, "."), func(x, y string) int {
		i, _ := strconv.Atoi(x)
		j, _ := strconv.Atoi(y)
		return cmp.Compare(i, j)
	})
}

// buildVersion returns the version of a stream build: the X.Y.Z that the NVR
// of every operator bundle of it ART resolved carries, whatever its other
// images carry, as ART moves a stream to the next z without rebuilding
// unchanged images. With no bundle resolved, it is the X.Y.Z that the NVR of
// every image of it ART resolved, base image excluded, carries. It is "" when
// ART resolved none or they differ, as while ART switches the stream to
// another version. nvrs holds ART's resolved NVRs by image.
func buildVersion(app string, snap *model.ReleaseSnapshot, nvrs map[string]string) string {
	images := buildImages(app, snap)
	bundles := slices.DeleteFunc(slices.Clone(images), func(c model.SnapshotImage) bool {
		_, ok := nvrs[c.Image]
		return !ok || !strings.HasSuffix(c.Name, "-bundle")
	})
	if len(bundles) > 0 {
		images = bundles
	}
	version := ""
	for _, img := range images {
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

// nvrName returns an NVR's name, the fields before its version and release,
// or "".
func nvrName(nvr string) string {
	f := strings.Split(nvr, "-")
	if len(f) < 3 {
		return ""
	}
	return strings.Join(f[:len(f)-2], "-")
}

// CandidateDigests returns the digests of the images the Images rows of every
// unshipped version that is not archived show, its FBC candidate's and
// stream's: the page shows the CVEs of no other image.
func (s *Selector) CandidateDigests(ctx context.Context) ([]string, error) {
	versions, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	var digests []string
	for _, v := range versions {
		if v.Archived {
			continue
		}
		c, _, err := s.ReleaseCandidate(ctx, &v)
		if err != nil {
			return nil, err
		}
		if c.Shipped {
			continue
		}
		for _, f := range []*model.FBCCandidate{c.FBCCandidate, c.FBCStream} {
			if f == nil {
				continue
			}
			for _, o := range f.Operators {
				for _, img := range o.Images {
					if img.Digest != "" && !slices.Contains(digests, img.Digest) {
						digests = append(digests, img.Digest)
					}
				}
			}
		}
	}
	return digests, nil
}

// candidateBuild returns the version's selected STAGE build, or nil when it
// has none, with its images' ART builds.
func (s *Selector) candidateBuild(ctx context.Context, release *model.ReleaseVersion) (*model.CandidateBuild, error) {
	staged, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil || staged == nil {
		return nil, err
	}
	snap, err := s.db.GetReleaseSnapshot(ctx, staged.SnapshotName)
	if err != nil {
		return nil, err
	}
	c := &model.CandidateBuild{Snapshot: snap.Name}
	if c.Components, err = s.buildComponents(ctx, release.KonfluxApplication, snap); err != nil {
		return nil, err
	}
	return c, nil
}

// buildComponents returns snap's images, base image excluded, with their ART
// builds.
func (s *Selector) buildComponents(ctx context.Context, app string, snap *model.ReleaseSnapshot) ([]model.CandidateComponent, error) {
	images := buildImages(app, snap)
	digests := make([]string, len(images))
	for i, img := range images {
		digests[i] = ImageDigest(img.Image)
	}
	builds, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	components := make([]model.CandidateComponent, len(images))
	for i, img := range images {
		components[i] = model.CandidateComponent{Name: img.Name, Image: img.Image}
		if b, ok := builds[digests[i]]; ok {
			components[i].NVR, components[i].BuildURL = b.NVR, artbuild.PageURL(s.ArtBaseURL, b.NVR, b.RecordID)
		}
	}
	return components, nil
}

// buildImages returns snap's component images less the base image
// (<application>-base-*), which ART's assembly Snapshots leave out.
func buildImages(app string, snap *model.ReleaseSnapshot) []model.SnapshotImage {
	return slices.DeleteFunc(slices.Clone(snap.Components), func(c model.SnapshotImage) bool {
		return strings.HasPrefix(c.Name, app+"-base-")
	})
}

// ImageDigest returns the digest of a repo@sha256:... image, or "" for a tag.
func ImageDigest(image string) string {
	_, d, _ := strings.Cut(image, "@")
	return d
}
