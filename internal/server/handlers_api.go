package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/release"
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
		Problems []syncstatus.Problem   `json:"problems"`
		Sources  []syncstatus.Freshness `json:"sources"`
	}{s.syncStatus.Problems(time.Now()), s.syncStatus.Sources()})
}

// --- Releases (version-centric) ---

func (s *Server) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	mr, err := s.db.ShipmentMR(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*model.ReleaseVersion
		ShipmentMR *model.ShipmentMR `json:"shipment_mr"`
	}{release, mr})
}

func (s *Server) handleListReleaseIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := s.db.ListJiraIssues(r.Context(), r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if issues == nil {
		issues = []model.JiraIssueRecord{}
	}
	writeJSON(w, http.StatusOK, issues)
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
func (s *Server) latestBuild(ctx context.Context, rel *model.ReleaseVersion, streams map[string]streamBuilds) (*time.Time, error) {
	app := rel.KonfluxApplication
	m := quayVersion.FindStringSubmatch(rel.Name)
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
	if builds := release.VersionBuilds(app, st.builds, st.nvrs, m[1]); len(builds) > 0 {
		latest = &builds[0].CreatedAt
	}
	staged, err := s.db.SelectedStageBuild(ctx, rel, s.selector.StageReleasePlanPattern)
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

func (s *Server) catalogShipped(name string) bool {
	return s.selector.CatalogShipped(name)
}

func (s *Server) handleReleasesOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	releases, err := s.db.ListAllReleaseVersions(ctx)
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

	mrs, err := s.db.ShipmentMRs(ctx)
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
			Readiness:    computeReadiness(&rel, latest != nil, catalogShipped, time.Now()),
			LatestBuild:  latest,
			Shipped:      catalogShipped || rel.Released,
			ShipmentMR:   mrs[rel.Name],
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
// whether a build snapshot exists, and whether the catalog already
// publishes it (JIRA can lag a shipped release). A version with nothing to
// flag gets an empty signal and message.
func computeReadiness(release *model.ReleaseVersion, hasSnapshot, catalogShipped bool, now time.Time) model.ReadinessResponse {
	if release.Released {
		return model.ReadinessResponse{Signal: "green", Message: "Released"}
	}
	if catalogShipped {
		return model.ReadinessResponse{Signal: "green", Message: "Shipped"}
	}

	var signal, message string

	// Due dates are midnight UTC. Count whole UTC days, as the version page
	// does, so a version is due today until its due day ends.
	const day = 24 * time.Hour
	var daysUntil int
	if release.DueDate != nil {
		daysUntil = int(release.DueDate.Truncate(day).Sub(now.Truncate(day)) / day)
	}
	// Builds are found only for a version ART builds, a quay-vX.Y.Z with a
	// Konflux application; no other version ever has one.
	tracked := release.KonfluxApplication != "" && quayVersion.MatchString(release.Name)

	if release.DueDate != nil && daysUntil < 0 {
		signal = "red"
		message = "Past due date"
	} else if tracked && !hasSnapshot {
		signal = "yellow"
		message = "No image snapshot yet"
	} else if release.DueDate != nil && daysUntil <= 3 {
		signal = "yellow"
		switch daysUntil {
		case 0:
			message = "Due today"
		case 1:
			message = "Due in 1 day"
		default:
			message = fmt.Sprintf("Due in %d days", daysUntil)
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
