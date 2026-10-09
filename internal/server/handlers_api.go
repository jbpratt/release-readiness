package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/github"
	"github.com/quay/release-readiness/internal/jira"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/releaseview"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	// The JIRA sync is tracked only when the server was started with a token.
	writeJSON(w, http.StatusOK, map[string]any{
		"jira_base_url": s.jiraBaseURL,
		"jira_project":  s.jiraProject,
		"jira_enabled":  s.syncStatus.Tracks("jira"),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Problems []syncstatus.Problem `json:"problems"`
	}{s.syncStatus.Problems(time.Now())})
}

// --- Releases (version-centric) ---

func (s *Server) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	writeJSON(w, http.StatusOK, release)
}

func (s *Server) handleListReleaseSnapshots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	q := r.URL.Query()
	apps := releaseview.Applications(release.KonfluxApplication)
	if app := q.Get("application"); app != "" {
		if !slices.Contains(apps, app) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("application %q is not part of release %q", app, version))
			return
		}
		apps = []string{app}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 25
	}
	limit = min(limit, 100)
	offset = max(offset, 0)
	// One extra row tells whether another page exists.
	snapshots, err := s.db.ListReleaseSnapshots(ctx, release.KonfluxApplication, apps, q.Get("with_release") == "true", limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	page := model.ReleaseSnapshotPage{Snapshots: snapshots, HasMore: len(snapshots) > limit}
	if page.HasMore {
		page.Snapshots = snapshots[:limit]
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleGetReleaseSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	release, snap, ok := s.releaseSnapshot(w, r)
	if !ok {
		return
	}
	if err := s.attachArtBuilds(ctx, snap); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.attachFBCCatalog(ctx, release, snap); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

var quayYStream = regexp.MustCompile(`^quay-(\d+)-(\d+)$`)

// attachFBCCatalog compares the Snapshot's quay-operator bundle with the newest
// stored quay-operator FBC catalog of the release, by full sha256 digest:
// the catalog names registry.redhat.io where Konflux names quay.io. It never
// reads the registry, and an unread or failed catalog is unknown.
func (s *Server) attachFBCCatalog(ctx context.Context, release *model.ReleaseVersion, snap *model.ReleaseSnapshot) error {
	app := release.KonfluxApplication
	m := quayYStream.FindStringSubmatch(app)
	i := slices.IndexFunc(snap.Components, func(c model.SnapshotImage) bool { return c.Name == app+"-quay-operator-bundle" })
	if m == nil || i < 0 {
		return nil
	}
	fc := &model.FBCCatalog{Status: "unknown", SnapshotBundleImage: snap.Components[i].Image}
	snap.FBCCatalog = fc
	cat, err := s.db.LatestFBCCatalog(ctx, "fbc-"+app, "fbc-"+app+"-quay-operator", "stable-"+m[1]+"."+m[2])
	if err != nil || cat == nil {
		return err
	}
	fc.CatalogSnapshot = cat.Snapshot
	_, want, _ := strings.Cut(fc.SnapshotBundleImage, "@")
	if cat.State != fbc.StateParsed || len(cat.Bundles) == 0 || !strings.HasPrefix(want, "sha256:") {
		return nil
	}
	// Behind shows the catalog's bundle of this z-stream, if it has one.
	name := fbc.Package + ".v" + strings.TrimPrefix(release.Name, "quay-v")
	behind := true
	for _, b := range cat.Bundles {
		if b.Digest == want {
			fc.Status, fc.CatalogBundleImage = "current", b.Image
			return nil
		}
		if b.Name == name {
			fc.CatalogBundleImage = b.Image
		}
		// A tag ref may name the bundle, so the channel cannot prove it absent.
		behind = behind && b.Digest != ""
	}
	if behind {
		fc.Status = "behind"
	}
	return nil
}

// releaseSnapshot loads the {version} release and its {name} Snapshot, or
// writes the error and returns false.
func (s *Server) releaseSnapshot(w http.ResponseWriter, r *http.Request) (*model.ReleaseVersion, *model.ReleaseSnapshot, bool) {
	ctx := r.Context()
	version, name := r.PathValue("version"), r.PathValue("name")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return nil, nil, false
	}
	snap, err := s.db.GetReleaseSnapshot(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("snapshot %q not found", name))
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return nil, nil, false
	}
	components := make([]string, len(snap.Components))
	for i, c := range snap.Components {
		components[i] = c.Name
	}
	if !releaseview.Contains(release.KonfluxApplication, snap.Application, components) {
		writeError(w, http.StatusNotFound, fmt.Errorf("snapshot %q is not part of release %q", name, version))
		return nil, nil, false
	}
	return release, snap, true
}

// pendingFresh is two ART resolver passes: a pending build not seen by a
// search since then is hidden while the service is unreachable.
const pendingFresh = 10 * time.Minute

// attachArtBuilds sets each component's cached ART build links, and a newer
// build of it still running when one started after the Snapshot from another
// upstream commit. It never calls the build history service.
func (s *Server) attachArtBuilds(ctx context.Context, snap *model.ReleaseSnapshot) error {
	if s.artBaseURL == "" {
		return nil
	}
	digests := make([]string, len(snap.Components))
	for i, c := range snap.Components {
		_, digests[i], _ = strings.Cut(c.Image, "@")
	}
	builds, err := s.db.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return err
	}
	var versions []string
	for _, b := range builds {
		if _, v := artbuild.SplitNVR(b.NVR); v != "" && !slices.Contains(versions, v) {
			versions = append(versions, v)
		}
	}
	pending, err := s.db.ArtPendingBuilds(ctx, versions, time.Now().Add(-pendingFresh))
	if err != nil {
		return err
	}
	for i, d := range digests {
		b, ok := builds[d]
		if !ok {
			continue
		}
		c := &snap.Components[i]
		c.Art = &model.ArtBuild{
			NVR:          b.NVR,
			BuildURL:     artbuild.PageURL(s.artBaseURL, b.NVR, b.RecordID),
			UpstreamRepo: b.UpstreamRepo,
			UpstreamSHA:  b.UpstreamSHA,
		}
		name, version := artbuild.SplitNVR(b.NVR)
		j := slices.IndexFunc(pending, func(p artbuild.PendingBuild) bool { return p.Name == name && p.Version == version })
		if j < 0 || b.UpstreamSHA == "" {
			continue
		}
		if p := pending[j]; p.UpstreamSHA != b.UpstreamSHA && p.StartedAt.After(snap.CreatedAt) {
			c.PendingArtBuild = &model.PendingArtBuild{
				BuildURL:    artbuild.PageURL(s.artBaseURL, p.NVR, p.RecordID),
				UpstreamSHA: p.UpstreamSHA,
				StartedAt:   p.StartedAt,
			}
		}
	}
	return nil
}

// handleListBuildAttempts returns the release's ART image-build attempts
// from the stored history of its stream. It never calls the service.
func (s *Server) handleListBuildAttempts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	resp := model.BuildAttempts{Attempts: []model.BuildAttempt{}}
	m := quayYStream.FindStringSubmatch(release.KonfluxApplication)
	if m == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	group := "quay-" + m[1] + "." + m[2]
	from, ok, err := s.db.ArtBuildCoverage(ctx, group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.CoveredFrom = &from
	attempts, err := s.db.ArtBuildAttempts(ctx, group, strings.TrimPrefix(release.Name, "quay-v"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, a := range attempts {
		resp.Attempts = append(resp.Attempts, model.BuildAttempt{
			Component: a.Name,
			Outcome:   a.Outcome,
			StartedAt: a.StartedAt,
			BuildURL:  artbuild.PageURL(s.artBaseURL, a.NVR, a.RecordID),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetStaged returns the newest image and FBC Snapshots ART staged for
// the release's assembly: version quay-v3.18.1 is assembly 3.18.1.
func (s *Server) handleGetStaged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	resp := model.StagedSnapshots{Assembly: strings.TrimPrefix(release.Name, "quay-v")}
	if resp.Image, err = s.db.LatestStagedSnapshot(ctx, resp.Assembly, "image"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if resp.FBC, err = s.db.LatestStagedSnapshot(ctx, resp.Assembly, "fbc"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetSelectedBuild returns the release's selected STAGE build, or
// {selected_build: null, reason} when none can be selected.
func (s *Server) handleGetSelectedBuild(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	builds, err := s.db.SelectedStageBuilds(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if builds.Selected == nil {
		writeJSON(w, http.StatusOK, map[string]any{"selected_build": nil, "reason": builds.Reason})
		return
	}
	writeJSON(w, http.StatusOK, builds.Selected)
}

// componentSet counts the release's newest image per component across its
// applications and returns when the newest of them was built. The images may
// come from different snapshots, so they are not one coherent build.
func componentSet(release *model.ReleaseVersion, candidates []releaseview.Component) (count int, latest *time.Time) {
	selected := releaseview.Select(release.KonfluxApplication, candidates)
	for _, c := range selected {
		if latest == nil || c.CreatedAt.After(*latest) {
			latest = &c.CreatedAt
		}
	}
	return len(selected), latest
}

func (s *Server) handleListReleaseIssues(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	issues, err := s.db.ListJiraIssues(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if issues == nil {
		issues = []model.JiraIssueRecord{}
	}
	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) handleGetReleaseIssueSummary(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	summary, err := s.db.GetIssueSummary(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleGetReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")

	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}

	issueSummary, _ := s.db.GetIssueSummary(ctx, version)

	candidates, err := s.db.ListComponentCandidates(ctx, releaseview.Applications(release.KonfluxApplication))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	count, _ := componentSet(release, candidates)

	writeJSON(w, http.StatusOK, computeReadiness(release, issueSummary, count > 0, s.catalogShipped(release.Name)))
}

// catalogShipped reports whether the catalog publishes the fixVersion, e.g.
// quay-v3.17.5 as quay 3.17.5.
func (s *Server) catalogShipped(name string) bool {
	product, version, _ := strings.Cut(name, "-v")
	return s.shipped.Has(product, version)
}

func (s *Server) handleReleasesOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	releases, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if releases == nil {
		releases = []model.ReleaseVersion{}
	}

	var apps []string
	for _, rel := range releases {
		apps = append(apps, releaseview.Applications(rel.KonfluxApplication)...)
	}
	slices.Sort(apps)
	candidates, err := s.db.ListComponentCandidates(ctx, slices.Compact(apps))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	fixVersions := make([]string, len(releases))
	for i, rel := range releases {
		fixVersions[i] = rel.Name
	}
	issueSummaries, err := s.db.GetIssueSummariesBatch(ctx, fixVersions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	overviews := make([]model.ReleaseOverview, len(releases))
	for i, rel := range releases {
		summary := issueSummaries[rel.Name]
		count, latest := componentSet(&rel, candidates)
		catalogShipped := s.catalogShipped(rel.Name)
		overviews[i] = model.ReleaseOverview{
			Release:      rel,
			IssueSummary: summary,
			Readiness:    computeReadiness(&rel, summary, count > 0, catalogShipped),
			LatestBuild:  latest,
			Shipped:      catalogShipped || rel.Released,
		}
	}
	markNextInStream(overviews)

	writeJSON(w, http.StatusOK, overviews)
}

// markNextInStream flags the lowest unshipped z of each product and
// major.minor, e.g. quay 3.18 for quay-v3.18.2. A name that does not parse
// is a stream of its own.
func markNextInStream(overviews []model.ReleaseOverview) {
	type candidate struct{ i, z int }
	next := make(map[string]candidate)
	for i, ov := range overviews {
		if ov.Shipped {
			continue
		}
		stream, z := ov.Release.Name, 0
		product, version, _ := strings.Cut(ov.Release.Name, "-v")
		if parts := strings.Split(version, "."); len(parts) == 3 {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				stream, z = product+" "+parts[0]+"."+parts[1], n
			}
		}
		if c, ok := next[stream]; !ok || z < c.z {
			next[stream] = candidate{i, z}
		}
	}
	for _, c := range next {
		overviews[c.i].NextInStream = true
	}
}

// computeReadiness derives a readiness signal from release metadata,
// issue summary, whether a build snapshot exists, and whether the catalog
// already publishes it (JIRA can lag a shipped release).
func computeReadiness(release *model.ReleaseVersion, issueSummary *model.IssueSummary, hasSnapshot, catalogShipped bool) model.ReadinessResponse {
	if release.Released {
		return model.ReadinessResponse{Signal: "green", Message: "Released"}
	}
	if catalogShipped {
		return model.ReadinessResponse{Signal: "green", Message: "Shipped"}
	}

	now := time.Now()
	signal := "green"
	message := "No open issues"

	openIssues := issueSummary != nil && issueSummary.Open > 0

	if release.DueDate != nil && now.After(*release.DueDate) {
		signal = "red"
		message = "Past due date"
	} else if openIssues {
		signal = "yellow"
		message = "Open issues remain"
	} else if !hasSnapshot {
		signal = "yellow"
		message = "No build snapshots yet"
	} else if release.DueDate != nil {
		daysUntil := int(release.DueDate.Sub(now).Hours() / 24)
		if daysUntil <= 3 {
			signal = "yellow"
			message = fmt.Sprintf("Due date in %d days", daysUntil)
		}
	}

	return model.ReadinessResponse{Signal: signal, Message: message}
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusOK {
		w.Header().Set("Cache-Control", "max-age=30")
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

const (
	reasonNoSelectedBuild = "no selected build"
	reasonNoEvidence      = "no build evidence"
	reasonScanIncomplete  = "selected build scan incomplete"
	reasonEarlierRange    = "earlier build range not compared"
	reasonNotCarried      = "inclusion not proven: a later range is incomplete"
	reasonNoConcrete      = "no reviewed concrete target version"
	reasonOutsideTarget   = "evidenced key outside reviewed target version"
	reasonNotScanned      = "not scanned yet"
)

// ticketHit is a ticket key's evidence in builds[idx] of a STAGE build
// history. Proven is set when every later range of that component was
// compared, so the newest build still contains it.
type ticketHit struct {
	key    string
	idx    int
	proven bool
	model.TicketEvidence
}

// walkEvidence collects the ticket evidence of builds[0]'s components through
// builds, newest first, and returns builds[0]'s components whose own range,
// or any earlier range, was not compared.
func (s *Server) walkEvidence(ctx context.Context, builds []*model.SelectedBuild) ([]ticketHit, []model.UnknownSpan, error) {
	scans := make([]map[[2]string]github.Scan, len(builds))
	for i, b := range builds {
		list, err := s.db.ListEvidenceScans(ctx, b.SnapshotName)
		if err != nil {
			return nil, nil, err
		}
		scans[i] = make(map[[2]string]github.Scan, len(list))
		for _, sc := range list {
			scans[i][[2]string{sc.Component, sc.ImageDigest}] = sc
		}
	}
	upstreamSHA := func(i int, name string) string {
		if i < len(builds) {
			if j := slices.IndexFunc(builds[i].Components, func(bc model.SelectedBuildComponent) bool { return bc.Name == name }); j >= 0 {
				return builds[i].Components[j].UpstreamSHA
			}
		}
		return ""
	}
	var hits []ticketHit
	var unknown []model.UnknownSpan
	for _, c := range builds[0].Components {
		chained := true
		for i, b := range builds {
			j := slices.IndexFunc(b.Components, func(bc model.SelectedBuildComponent) bool { return bc.Name == c.Name })
			if j < 0 {
				break
			}
			digest := b.Components[j].ImageDigest
			sc, ok := scans[i][[2]string{c.Name, digest}]
			// A re-released Snapshot moves in the history: its stored range
			// proves nothing once it no longer matches its neighbours.
			if ok && sc.State == github.StateComplete && (sc.HeadSHA != b.Components[j].UpstreamSHA || sc.BaseSHA != upstreamSHA(i+1, c.Name)) {
				sc, ok = github.Scan{}, false
			}
			for _, e := range sc.Evidence {
				hits = append(hits, ticketHit{key: e.Key, idx: i, proven: chained, TicketEvidence: model.TicketEvidence{
					Snapshot: b.SnapshotName, Component: c.Name, ImageDigest: digest, CommitSHA: e.CommitSHA, CommitURL: e.CommitURL, PRURL: e.PRURL,
				}})
			}
			if ok && sc.State == github.StateComplete {
				continue
			}
			if i == 0 {
				reason := sc.Reason
				if !ok {
					reason = reasonNotScanned
				}
				unknown = append(unknown, model.UnknownSpan{Component: c.Name, ImageDigest: digest, Reason: reason})
			} else if chained {
				unknown = append(unknown, model.UnknownSpan{Component: c.Name, ImageDigest: c.ImageDigest, Reason: reasonEarlierRange})
			}
			chained = false
		}
	}
	return hits, unknown, nil
}

// firstProven maps each key with proven evidence to the oldest build index
// proving it.
func firstProven(hits []ticketHit) map[string]int {
	first := make(map[string]int)
	for _, h := range hits {
		if f, ok := first[h.key]; h.proven && (!ok || h.idx > f) {
			first[h.key] = h.idx
		}
	}
	return first
}

// jiraAction is the display-only Jira action for a proven concrete ticket.
func jiraAction(status string) (action, reason string) {
	switch strings.ToLower(status) {
	case "verified":
		return "would_move_to_release_pending", ""
	case "release pending", "closed":
		return "none", ""
	case "":
		return "skipped", "status_unknown"
	}
	return "skipped", "status_not_verified"
}

// handleGetTicketMembership puts each ticket of the release's concrete Target
// Version, its .z stream, and its selected STAGE build's commits in one
// bucket. A reviewed Target Version is never proof of membership.
func (s *Server) handleGetTicketMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	history, err := s.db.StageBuildHistory(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := model.TicketMembership{Buckets: model.TicketBuckets{
		InSelectedBuild: []model.TicketRow{}, CarriedForward: []model.TicketRow{}, PlannedPatchPending: []model.TicketRow{}, NeedsReview: []model.TicketRow{},
	}}
	var hits []ticketHit
	var unknown []model.UnknownSpan
	if len(history) > 0 {
		resp.SelectedBuild = history[0]
		if hits, unknown, err = s.walkEvidence(ctx, history); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		builds, err := s.db.SelectedStageBuilds(ctx, release, s.StageReleasePlanPattern)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		resp.Reason = builds.Reason
	}

	issues := make(map[string]model.JiraIssueRecord)
	evidence := make(map[string][]model.TicketEvidence)
	for _, h := range hits {
		evidence[h.key] = append(evidence[h.key], h.TicketEvidence)
	}
	// Concrete rows are read last so they win over the stream's.
	for _, v := range []string{jira.StreamVersion(release.Name), release.Name} {
		if v == "" {
			continue
		}
		list, err := s.db.ListJiraIssues(ctx, v)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, i := range list {
			issues[i.Key] = i
		}
	}
	keys := slices.Collect(maps.Keys(issues))
	for k := range evidence {
		if _, ok := issues[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)

	incomplete := reasonEarlierRange
	if slices.ContainsFunc(unknown, func(u model.UnknownSpan) bool { return u.Reason != reasonEarlierRange }) {
		incomplete = reasonScanIncomplete
	}
	first := firstProven(hits)
	for _, key := range keys {
		issue, listed := issues[key]
		row := model.TicketRow{
			Key: key, Link: s.jiraBaseURL + "/browse/" + key, Summary: issue.Summary, Status: issue.Status,
			TargetVersion: issue.FixVersion, ConcreteVersion: release.Name,
			EvidenceState: "unknown", EvidenceReason: reasonNoSelectedBuild, Evidence: evidence[key],
			JiraAction: "skipped",
		}
		if row.Evidence == nil {
			row.Evidence = []model.TicketEvidence{}
		}
		f, proven := first[key]
		if resp.SelectedBuild != nil {
			row.CurrentSnapshot = resp.SelectedBuild.SnapshotName
			switch {
			case proven:
				row.EvidenceState, row.EvidenceReason = "complete", ""
				row.FirstSeenSnapshot = history[f].SnapshotName
			case len(row.Evidence) > 0:
				row.EvidenceReason = reasonNotCarried
			case len(unknown) > 0:
				row.EvidenceReason = incomplete
			default:
				row.EvidenceState, row.EvidenceReason = "none", reasonNoEvidence
			}
		}
		bucket := &resp.Buckets.NeedsReview
		row.Bucket = "needs_review"
		switch {
		case !listed:
			row.EvidenceReason = reasonOutsideTarget
			row.JiraActionReason = reasonOutsideTarget
		case issue.FixVersion != release.Name:
			bucket, row.Bucket = &resp.Buckets.PlannedPatchPending, "planned_patch_pending"
			row.JiraActionReason = reasonNoConcrete
		case row.EvidenceState == "complete" && f == 0:
			bucket, row.Bucket = &resp.Buckets.InSelectedBuild, "in_selected_build"
			row.JiraAction, row.JiraActionReason = jiraAction(issue.Status)
		case row.EvidenceState == "complete":
			bucket, row.Bucket = &resp.Buckets.CarriedForward, "carried_forward"
			row.JiraAction, row.JiraActionReason = jiraAction(issue.Status)
		default:
			row.JiraActionReason = row.EvidenceReason
		}
		*bucket = append(*bucket, row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetTicketDelta returns the tickets the named STAGE build Snapshot
// adds over the STAGE build before it, and the ones it carries forward.
func (s *Server) handleGetTicketDelta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	release, snap, ok := s.releaseSnapshot(w, r)
	if !ok {
		return
	}
	history, err := s.db.StageBuildHistory(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := model.TicketDelta{Snapshot: snap.Name, DeltaState: "unknown", Added: []model.DeltaTicket{}, Carried: []model.DeltaTicket{}, Unknown: []model.UnknownSpan{}}
	i := slices.IndexFunc(history, func(b *model.SelectedBuild) bool { return b.SnapshotName == snap.Name })
	switch {
	case i < 0:
		resp.Reason = "not a successful stage build of this version"
		writeJSON(w, http.StatusOK, resp)
		return
	case i == len(history)-1:
		resp.Reason = "no previous stage build"
	default:
		resp.Baseline = history[i+1].SnapshotName
		for _, c := range history[i+1].Components {
			if !slices.ContainsFunc(history[i].Components, func(bc model.SelectedBuildComponent) bool { return bc.Name == c.Name }) {
				resp.Unknown = append(resp.Unknown, model.UnknownSpan{Component: c.Name, ImageDigest: c.ImageDigest, Reason: "component removed since baseline"})
			}
		}
	}
	hits, unknown, err := s.walkEvidence(ctx, history[i:])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp.Unknown = append(resp.Unknown, unknown...)
	first := firstProven(hits)
	for _, h := range hits {
		if f, ok := first[h.key]; !ok || !h.proven || h.idx != f {
			continue
		}
		t := model.DeltaTicket{Key: h.key, FirstSeenSnapshot: h.Snapshot, TicketEvidence: h.TicketEvidence}
		if h.idx == 0 {
			resp.Added = append(resp.Added, t)
		} else {
			resp.Carried = append(resp.Carried, t)
		}
	}
	byKey := func(a, b model.DeltaTicket) int { return strings.Compare(a.Key, b.Key) }
	slices.SortStableFunc(resp.Added, byKey)
	slices.SortStableFunc(resp.Carried, byKey)
	if resp.Baseline != "" && len(resp.Unknown) == 0 {
		resp.DeltaState = "complete"
	}
	writeJSON(w, http.StatusOK, resp)
}
