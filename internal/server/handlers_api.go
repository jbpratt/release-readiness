package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/releaseview"
)

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"jira_base_url": s.jiraBaseURL,
		"jira_project":  s.jiraProject,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// --- Snapshots ---

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	snapshots, err := s.db.ListSnapshots(r.Context(), q.Get("application"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

// --- Konflux Releases ---

func (s *Server) handleListKonfluxReleases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	releases, err := s.db.ListKonfluxReleases(r.Context(), q.Get("application"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, releases)
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

func (s *Server) handleGetReleaseComponents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	components, err := s.releaseComponents(ctx, release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, components)
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
	version, name := r.PathValue("version"), r.PathValue("name")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	snap, err := s.db.GetReleaseSnapshot(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("snapshot %q not found", name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	components := make([]string, len(snap.Components))
	for i, c := range snap.Components {
		components[i] = c.Name
	}
	if !releaseview.Contains(release.KonfluxApplication, snap.Application, components) {
		writeError(w, http.StatusNotFound, fmt.Errorf("snapshot %q is not part of release %q", name, version))
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) releaseComponents(ctx context.Context, release *model.ReleaseVersion) (model.ReleaseComponents, error) {
	candidates, err := s.db.ListComponentCandidates(ctx, releaseview.Applications(release.KonfluxApplication))
	if err != nil {
		return model.ReleaseComponents{}, err
	}
	return toReleaseComponents(release, candidates), nil
}

func toReleaseComponents(release *model.ReleaseVersion, candidates []releaseview.Component) model.ReleaseComponents {
	selected := releaseview.Select(release.KonfluxApplication, candidates)
	rc := model.ReleaseComponents{Release: release.Name, Components: make([]model.ReleaseComponent, len(selected))}
	for i, c := range selected {
		rc.Components[i] = model.ReleaseComponent{
			Name:        c.Name,
			Image:       c.Image,
			GitSHA:      c.GitSHA,
			GitURL:      c.GitURL,
			Application: c.Application,
			Snapshot:    c.Snapshot,
			CreatedAt:   c.CreatedAt,
		}
		if rc.AsOf == nil || c.CreatedAt.After(*rc.AsOf) {
			rc.AsOf = &c.CreatedAt
		}
	}
	return rc
}

func (s *Server) handleListReleaseIssues(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	q := r.URL.Query()
	issues, err := s.db.ListJiraIssues(r.Context(), version, q.Get("type"), q.Get("status"), q.Get("label"))
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

	components, err := s.releaseComponents(ctx, release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, computeReadiness(release, issueSummary, len(components.Components) > 0))
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
		components := toReleaseComponents(&rel, candidates)
		overviews[i] = model.ReleaseOverview{
			Release:        rel,
			IssueSummary:   summary,
			Readiness:      computeReadiness(&rel, summary, len(components.Components) > 0),
			ComponentCount: len(components.Components),
			LatestBuild:    components.AsOf,
		}
	}

	writeJSON(w, http.StatusOK, overviews)
}

// computeReadiness derives a readiness signal from release metadata,
// issue summary, and whether a build snapshot exists.
func computeReadiness(release *model.ReleaseVersion, issueSummary *model.IssueSummary, hasSnapshot bool) model.ReadinessResponse {
	if release.Released {
		return model.ReadinessResponse{Signal: "green", Message: "Released"}
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
