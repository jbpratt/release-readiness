package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/quay/release-readiness/internal/model"
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

func (s *Server) handleGetReleaseSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}

	if release.S3Application == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("no S3 application mapped for release %q", version))
		return
	}

	// Get the latest snapshot for this release's S3 application
	apps, err := s.db.LatestSnapshotPerApplication(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for _, app := range apps {
		if app.Application == release.S3Application {
			if app.LatestSnapshot == nil {
				writeError(w, http.StatusNotFound, fmt.Errorf("no snapshots found for %s", release.S3Application))
				return
			}
			// Get full snapshot with components and test results
			snap, err := s.db.GetSnapshotByName(ctx, app.LatestSnapshot.Name)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, snap)
			return
		}
	}

	writeError(w, http.StatusNotFound, fmt.Errorf("no snapshots found for application %s", release.S3Application))
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

	hasSnapshot := false
	if release.S3Application != "" {
		apps, err := s.db.LatestSnapshotPerApplication(ctx)
		if err == nil {
			for _, app := range apps {
				if app.Application == release.S3Application && app.LatestSnapshot != nil {
					hasSnapshot = true
					break
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, computeReadiness(release, issueSummary, hasSnapshot))
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

	apps, err := s.db.LatestSnapshotPerApplication(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	snapshotMap := make(map[string]*model.SnapshotRecord, len(apps))
	for i := range apps {
		if apps[i].LatestSnapshot != nil {
			snapshotMap[apps[i].Application] = apps[i].LatestSnapshot
		}
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
		var snap *model.SnapshotRecord
		if rel.S3Application != "" {
			if s := snapshotMap[rel.S3Application]; s != nil {
				// Return snapshot metadata only (no components)
				snapCopy := *s
				snapCopy.Components = nil
				snap = &snapCopy
			}
		}

		overviews[i] = model.ReleaseOverview{
			Release:      rel,
			IssueSummary: summary,
			Readiness:    computeReadiness(&rel, summary, snap != nil),
			Snapshot:     snap,
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
