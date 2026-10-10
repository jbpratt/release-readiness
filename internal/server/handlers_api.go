package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// handleGetStaged returns the newest image Snapshot and each operator's newest
// FBC Snapshot ART staged for the release's assembly: version quay-v3.18.1 is
// assembly 3.18.1. ART stages one FBC per operator and OCP version.
func (s *Server) handleGetStaged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	var resp model.StagedSnapshots
	if resp.StreamStaged, err = s.db.StreamStaged(ctx, release.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	assembly := strings.TrimPrefix(release.Name, "quay-v")
	if resp.Image, err = s.db.LatestStagedSnapshot(ctx, assembly, "image", ""); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, op := range []string{"quay-operator", "container-security-operator", "quay-bridge-operator"} {
		c := model.StagedCatalog{Operator: op}
		if c.Staged, err = s.db.LatestStagedSnapshot(ctx, assembly, "fbc", "fbc-"+release.KonfluxApplication+"-"+op); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		resp.Catalogs = append(resp.Catalogs, c)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetBuildTickets lists the release's Target Version tickets, then the
// tickets of its .z stream that a commit of its selected STAGE build names,
// each with the build commits naming it. A ticket under both Target Versions
// is listed once, as the release's own.
func (s *Server) handleGetBuildTickets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	build, reason, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := model.BuildTickets{Build: build, Reason: reason, NotCompared: []model.NotCompared{}, Tickets: []model.BuildTicket{}}
	var found map[string][]model.BuildCommit
	if build != nil {
		found, resp.NotCompared = s.Scanner.Tickets(build)
		// The scanner skips released and archived versions, so "not scanned
		// yet" would never change for them.
		for i, c := range resp.NotCompared {
			if c.Reason == github.ReasonNotScanned && (release.Released || release.Archived) {
				resp.NotCompared[i].Reason = "version is released or archived"
			}
		}
	}
	issues, err := s.db.ListJiraIssues(ctx, release.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if z := jira.StreamVersion(release.Name); z != "" {
		stream, err := s.db.ListJiraIssues(ctx, z)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, i := range stream {
			listed := slices.ContainsFunc(issues, func(c model.JiraIssueRecord) bool { return c.Key == i.Key })
			if _, ok := found[i.Key]; ok && !listed {
				issues = append(issues, i)
			}
		}
	}
	for _, i := range issues {
		inBuild := found[i.Key]
		if inBuild == nil {
			inBuild = []model.BuildCommit{}
		}
		resp.Tickets = append(resp.Tickets, model.BuildTicket{JiraIssueRecord: i, InBuild: inBuild})
	}
	writeJSON(w, http.StatusOK, resp)
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
		return model.ReadinessResponse{Signal: "green", Message: "Released", Shipped: true}
	}
	if catalogShipped {
		return model.ReadinessResponse{Signal: "green", Message: "Shipped", Shipped: true}
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
