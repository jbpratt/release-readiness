package server

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/releaseview"
)

// prowRunsResponse carries runs plus when their jobs were last listed. Stale
// is set once any of those jobs has missed two poll intervals.
type prowRunsResponse struct {
	LastSuccessfulSync *time.Time `json:"last_successful_sync"`
	Stale              bool       `json:"stale"`
	Runs               []prow.Run `json:"runs"`
}

// snapshotProwRunsResponse maps each Snapshot component to the runs that
// tested its exact (role, digest), newest first.
type snapshotProwRunsResponse struct {
	LastSuccessfulSync *time.Time            `json:"last_successful_sync"`
	Stale              bool                  `json:"stale"`
	Components         map[string][]prow.Run `json:"components"`
}

// handleListReleaseProwRuns serves the runs of the release's Konflux
// application, so every z-stream of a minor shares them. unlinked=true keeps
// only runs that tested no image of any of the release's Snapshots.
func (s *Server) handleListReleaseProwRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(ctx, version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	app := release.KonfluxApplication
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	q := r.URL.Query()
	if q.Get("unlinked") == "true" {
		candidates, err := s.db.ListComponentCandidates(ctx, releaseview.Applications(app))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		keys := map[string]bool{}
		for _, c := range candidates {
			if k := prow.ComponentKey(c.Application, c.Name, c.Image); k != "" {
				keys[k] = true
			}
		}
		runs = slices.DeleteFunc(runs, func(run prow.Run) bool { return run.Tested(keys) })
	}
	syncs, err := s.db.ListProwSyncs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	start := min(max(offset, 0), len(runs))
	resp := prowRunsResponse{Runs: runs[start:min(start+min(limit, 200), len(runs))]}
	resp.LastSuccessfulSync, resp.Stale = syncSummary(syncs, app)
	writeJSON(w, http.StatusOK, resp)
}

// handleListSnapshotProwRuns serves, per component of one release Snapshot,
// the runs of the release's application that tested that exact image.
func (s *Server) handleListSnapshotProwRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	release, snap, ok := s.releaseSnapshot(w, r)
	if !ok {
		return
	}
	app := release.KonfluxApplication
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	syncs, err := s.db.ListProwSyncs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := snapshotProwRunsResponse{Components: map[string][]prow.Run{}}
	resp.LastSuccessfulSync, resp.Stale = syncSummary(syncs, app)
	for _, c := range snap.Components {
		matched := []prow.Run{}
		if k := prow.ComponentKey(snap.Application, c.Name, c.Image); k != "" {
			keys := map[string]bool{k: true}
			for _, run := range runs {
				if run.Tested(keys) {
					matched = append(matched, run)
				}
			}
		}
		resp.Components[c.Name] = matched
	}
	writeJSON(w, http.StatusOK, resp)
}

// syncSummary returns the oldest last sync of app's jobs and whether any of
// them has missed two poll intervals.
func syncSummary(syncs []prow.SyncState, app string) (last *time.Time, stale bool) {
	for _, st := range syncs {
		if st.Application != app {
			continue
		}
		if last == nil || st.LastSuccessfulSync.Before(*last) {
			last = &st.LastSuccessfulSync
		}
		if time.Since(st.LastSuccessfulSync) > 2*st.Interval {
			stale = true
		}
	}
	return last, stale
}
