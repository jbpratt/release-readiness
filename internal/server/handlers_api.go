package server

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	apps := slices.DeleteFunc(q["application"], func(a string) bool { return a == "" })
	if version := q.Get("release"); version != "" {
		release, err := s.db.GetReleaseVersion(r.Context(), version)
		if err != nil {
			writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
			return
		}
		apps = releaseApplications(release.S3Application)
		if len(apps) == 0 {
			writeJSON(w, http.StatusOK, []model.SnapshotRecord{})
			return
		}
	}
	snapshots, err := s.db.ListSnapshots(r.Context(), apps, limit, offset)
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

	apps, err := s.db.LatestSnapshotPerApplication(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	latest, _, _ := releaseSnapshot(latestByApplication(apps), release.S3Application)
	if latest == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no snapshots found for release %q", version))
		return
	}
	// Get full snapshot with components and test results
	snap, err := s.db.GetSnapshotByName(ctx, latest.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
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

	testsPassed := false
	hasTests := false
	if release.S3Application != "" {
		apps, err := s.db.LatestSnapshotPerApplication(ctx)
		if err == nil {
			_, testsPassed, hasTests = releaseSnapshot(latestByApplication(apps), release.S3Application)
		}
	}

	writeJSON(w, http.StatusOK, computeReadiness(release, issueSummary, testsPassed, hasTests))
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
	snapshotMap := latestByApplication(apps)

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
		latest, testsPassed, hasTests := releaseSnapshot(snapshotMap, rel.S3Application)
		if latest != nil {
			// Return snapshot metadata only (no components/test_results)
			snapCopy := *latest
			snapCopy.Components = nil
			snapCopy.TestSuites = nil
			snap = &snapCopy
		}

		overviews[i] = model.ReleaseOverview{
			Release:      rel,
			IssueSummary: summary,
			Readiness:    computeReadiness(&rel, summary, testsPassed, hasTests),
			Snapshot:     snap,
		}
	}

	writeJSON(w, http.StatusOK, overviews)
}

// releaseApplications returns the snapshot application names that belong to
// the release whose S3 application is s3App: the S3 name, the Konflux name
// (quay-v3-16 -> quay-3-16) and the Konflux fbc- name.
func releaseApplications(s3App string) []string {
	if s3App == "" {
		return nil
	}
	i := strings.LastIndex(s3App, "-v")
	if i <= 0 || i+2 == len(s3App) {
		return []string{s3App}
	}
	konflux := s3App[:i] + "-" + s3App[i+2:]
	return []string{s3App, konflux, "fbc-" + konflux}
}

func latestByApplication(apps []model.ApplicationSummary) map[string]*model.SnapshotRecord {
	m := make(map[string]*model.SnapshotRecord, len(apps))
	for _, app := range apps {
		if app.LatestSnapshot != nil {
			m[app.Application] = app.LatestSnapshot
		}
	}
	return m
}

// releaseSnapshot summarizes the latest snapshots of a release's applications.
// snap is the newest non-fbc one, since fbc is the catalog rather than the
// product image set; tests pass only if every latest snapshot with tests passed.
func releaseSnapshot(latest map[string]*model.SnapshotRecord, s3App string) (snap *model.SnapshotRecord, testsPassed, hasTests bool) {
	testsPassed = true
	for _, app := range releaseApplications(s3App) {
		s := latest[app]
		if s == nil {
			continue
		}
		if s.HasTests {
			hasTests = true
			testsPassed = testsPassed && s.TestsPassed
		}
		if !strings.HasPrefix(app, "fbc-") && (snap == nil || s.CreatedAt.After(snap.CreatedAt)) {
			snap = s
		}
	}
	return snap, testsPassed && hasTests, hasTests
}

// computeReadiness derives a readiness signal from release metadata,
// issue summary, and test status.
func computeReadiness(release *model.ReleaseVersion, issueSummary *model.IssueSummary, testsPassed, hasTests bool) model.ReadinessResponse {
	if release.Released {
		return model.ReadinessResponse{Signal: "green", Message: "Released"}
	}

	now := time.Now()
	signal := "green"
	message := "All checks passing"

	openIssues := issueSummary != nil && issueSummary.Open > 0
	testsFailing := hasTests && !testsPassed

	if release.DueDate != nil && now.After(*release.DueDate) {
		signal = "red"
		message = "Past due date"
	} else if testsFailing && openIssues {
		signal = "red"
		message = "Tests failing and open issues remain"
	} else if testsFailing {
		signal = "yellow"
		message = "Integration tests failing"
	} else if openIssues {
		signal = "yellow"
		message = "Open issues remain"
	} else if release.DueDate != nil {
		daysUntil := int(release.DueDate.Sub(now).Hours() / 24)
		if daysUntil <= 3 {
			signal = "yellow"
			message = fmt.Sprintf("Due date in %d days", daysUntil)
		}
	}

	return model.ReadinessResponse{Signal: signal, Message: message}
}

// --- Artifacts ---

func (s *Server) handleDownloadSuiteArtifacts(w http.ResponseWriter, r *http.Request) {
	if s.s3 == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("S3 not configured"))
		return
	}

	snapshotID, err := strconv.ParseInt(r.PathValue("snapshotId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid snapshot ID"))
		return
	}
	suiteID, err := strconv.ParseInt(r.PathValue("suiteId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid suite ID"))
		return
	}

	ctx := r.Context()

	snap, err := s.db.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("snapshot %d not found", snapshotID))
		return
	}

	suite, err := s.db.GetTestSuiteByID(ctx, suiteID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("test suite %d not found", suiteID))
		return
	}
	if suite.SnapshotID != snapshotID {
		writeError(w, http.StatusBadRequest, fmt.Errorf("suite %d does not belong to snapshot %d", suiteID, snapshotID))
		return
	}

	prefix := snap.Application + "/snapshots/" + snap.Name + "/" + suite.Name + "/"
	keys, err := s.s3.ListObjects(ctx, prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("listing artifacts: %w", err))
		return
	}
	if len(keys) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("no artifacts found for suite %q", suite.Name))
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-artifacts.tar.gz"`, suite.Name))

	gw := gzip.NewWriter(w)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	for _, key := range keys {
		body, size, err := s.s3.GetObjectStream(ctx, key)
		if err != nil {
			s.logger.Error("fetch artifact", "key", key, "error", err)
			continue
		}

		relPath := strings.TrimPrefix(key, prefix)
		if err := tw.WriteHeader(&tar.Header{
			Name: relPath,
			Size: size,
			Mode: 0o644,
		}); err != nil {
			_ = body.Close()
			s.logger.Error("write tar header", "key", key, "error", err)
			return
		}
		if _, err := io.Copy(tw, body); err != nil {
			_ = body.Close()
			s.logger.Error("write tar body", "key", key, "error", err)
			return
		}
		_ = body.Close()
	}
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
