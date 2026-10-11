package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/github"
	"github.com/quay/release-readiness/internal/jira"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	// The JIRA sync is tracked only when the server was started with a token.
	writeJSON(w, http.StatusOK, map[string]any{
		"jira_base_url":     s.jiraBaseURL,
		"jira_project":      s.jiraProject,
		"jira_enabled":      s.syncStatus.Tracks("jira"),
		"konflux_ui_url":    s.KonfluxUIURL,
		"konflux_namespace": s.KonfluxNamespace,
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

// handleGetBuildTickets lists the release's Target Version tickets, then the
// tickets of its .z stream that a commit of its candidate new since the
// previous version's candidate names, each with the candidate commits naming
// it and, for a Target Version ticket none names, the default-branch commits
// naming it that the candidate lacks. A ticket under both Target Versions is
// listed once, as the release's own.
func (s *Server) handleGetBuildTickets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	versions, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	build, reason, err := s.ticketBuild(ctx, release, versions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := model.BuildTickets{Build: build, Reason: reason, NotCompared: []model.NotCompared{}, Tickets: []model.BuildTicket{}}
	var e github.Evidence
	if build != nil {
		e = s.Scanner.Tickets(build)
		resp.NotCompared = e.NotCompared
		if e.ZSince {
			resp.ZSince = build.Since
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
			if e.Z[i.Key] && !listed {
				issues = append(issues, i)
			}
		}
	}
	for _, i := range issues {
		t := model.BuildTicket{JiraIssueRecord: i, InBuild: e.InBuild[i.Key], NotInBuild: []model.BuildCommit{}}
		if t.InBuild == nil {
			t.InBuild = []model.BuildCommit{}
		}
		// A .z ticket is listed only because a candidate commit names it.
		if missing, ok := e.NotInBuild[i.Key]; ok && i.FixVersion == release.Name {
			t.NotInBuild = missing
		}
		resp.Tickets = append(resp.Tickets, t)
	}
	writeJSON(w, http.StatusOK, resp)
}

// TicketBuilds returns the build each version's tickets are checked against,
// for the versions that have one.
func (s *Server) TicketBuilds(ctx context.Context) ([]model.TicketBuild, error) {
	versions, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	var builds []model.TicketBuild
	for i := range versions {
		b, _, err := s.ticketBuild(ctx, &versions[i], versions)
		if err != nil {
			return nil, err
		}
		if b != nil {
			builds = append(builds, *b)
		}
	}
	return builds, nil
}

// ticketBuild returns release's candidate as its tickets are checked against
// it, or nil and the reason there is none: "shipped", "archived" or the
// candidate's. When the previous version is unshipped and has a candidate,
// each component's base is the upstream commit of the component of the same
// name in that candidate.
func (s *Server) ticketBuild(ctx context.Context, release *model.ReleaseVersion, versions []model.ReleaseVersion) (*model.TicketBuild, string, error) {
	c, err := s.selectCandidate(ctx, release)
	switch {
	case err != nil:
		return nil, "", err
	case c.shipped:
		return nil, "shipped", nil
	case release.Archived:
		return nil, "archived", nil
	case c.build == nil:
		return nil, c.reason, nil
	}
	issues, err := s.db.ListJiraIssues(ctx, release.Name)
	if err != nil {
		return nil, "", err
	}
	b := &model.TicketBuild{Snapshot: c.build.Snapshot, Source: c.build.Source, Targets: len(issues) > 0}
	for _, cc := range c.build.Components {
		b.Components = append(b.Components, model.TicketComponent{Name: cc.Name, UpstreamRepo: cc.UpstreamRepo, UpstreamSHA: cc.UpstreamSHA})
	}
	prev := previousVersion(release, versions)
	if prev == nil {
		return b, "", nil
	}
	pc, err := s.selectCandidate(ctx, prev)
	if err != nil || pc.build == nil {
		return b, "", err
	}
	b.Since = prev.Name
	for i, tc := range b.Components {
		if j := slices.IndexFunc(pc.build.Components, func(p model.CandidateComponent) bool { return p.Name == tc.Name }); j >= 0 {
			b.Components[i].BaseSHA = pc.build.Components[j].UpstreamSHA
		}
	}
	return b, "", nil
}

// previousVersion returns the version of release's X.Y with the highest lower
// Z that is not archived, or nil.
func previousVersion(release *model.ReleaseVersion, versions []model.ReleaseVersion) *model.ReleaseVersion {
	xy, z, ok := splitZ(release.Name)
	if !ok {
		return nil
	}
	var prev *model.ReleaseVersion
	best := -1
	for i, v := range versions {
		if vxy, vz, ok := splitZ(v.Name); ok && !v.Archived && vxy == xy && vz < z && vz > best {
			prev, best = &versions[i], vz
		}
	}
	return prev
}

// splitZ returns the X.Y and the Z of a quay-vX.Y.Z version.
func splitZ(name string) (xy string, z int, ok bool) {
	m := quayVersion.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	i := strings.LastIndexByte(m[1], '.')
	z, err := strconv.Atoi(m[1][i+1:])
	return m[1][:i], z, err == nil
}

// streamBuilds is an application's stream builds, newest first, and the NVRs
// of their images, as db.StreamBuilds returns them.
type streamBuilds struct {
	builds []model.ReleaseSnapshot
	nvrs   map[string]string
}

// latestBuild returns when the version's newest build was created: the newer
// of its newest build and its selected STAGE build, or nil when it has
// neither. streams keeps each application's stream builds once read.
func (s *Server) latestBuild(ctx context.Context, release *model.ReleaseVersion, streams map[string]streamBuilds) (*time.Time, error) {
	app := release.KonfluxApplication
	m := quayVersion.FindStringSubmatch(release.Name)
	if app == "" || m == nil {
		return nil, nil
	}
	st, ok := streams[app]
	if !ok {
		builds, nvrs, err := s.db.StreamBuilds(ctx, app)
		if err != nil {
			return nil, err
		}
		st = streamBuilds{builds, nvrs}
		streams[app] = st
	}
	var latest *time.Time
	if builds := versionBuilds(app, st.builds, st.nvrs, m[1]); len(builds) > 0 {
		latest = &builds[0].CreatedAt
	}
	staged, _, err := s.db.SelectedStageBuild(ctx, release, s.StageReleasePlanPattern)
	if err != nil || staged == nil {
		return latest, err
	}
	snap, err := s.db.GetReleaseSnapshot(ctx, staged.SnapshotName)
	if err != nil {
		return nil, err
	}
	if latest == nil || snap.CreatedAt.After(*latest) {
		latest = &snap.CreatedAt
	}
	return latest, nil
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

	fixVersions := make([]string, len(releases))
	for i, rel := range releases {
		fixVersions[i] = rel.Name
	}
	issueSummaries, err := s.db.GetIssueSummariesBatch(ctx, fixVersions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	streams := map[string]streamBuilds{}
	overviews := make([]model.ReleaseOverview, len(releases))
	for i, rel := range releases {
		summary := issueSummaries[rel.Name]
		latest, err := s.latestBuild(ctx, &rel, streams)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		catalogShipped := s.catalogShipped(rel.Name)
		overviews[i] = model.ReleaseOverview{
			Release:      rel,
			IssueSummary: summary,
			Readiness:    computeReadiness(&rel, summary, latest != nil, catalogShipped),
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
