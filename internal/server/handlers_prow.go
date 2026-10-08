package server

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/quay/release-readiness/internal/prow"
)

// prowRunsResponse carries runs plus when their jobs were last listed. Stale
// is set once any of those jobs has missed two poll intervals.
type prowRunsResponse struct {
	LastSuccessfulSync *time.Time `json:"last_successful_sync"`
	Stale              bool       `json:"stale"`
	Runs               []prow.Run `json:"runs"`
}

func (s *Server) handleListReleaseProwRuns(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	limit, offset := prowPage(r)
	runs, err := s.db.ListProwRunsByRelease(r.Context(), version, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeProwRuns(w, r, runs, func(st prow.SyncState) bool { return st.ReleaseVersion == version })
}

func (s *Server) handleListProwRunsByDigest(w http.ResponseWriter, r *http.Request) {
	limit, offset := prowPage(r)
	runs, err := s.db.ListProwRunsByDigest(r.Context(), r.PathValue("digest"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeProwRuns(w, r, runs, func(st prow.SyncState) bool {
		return slices.ContainsFunc(runs, func(run prow.Run) bool { return run.JobName == st.JobName })
	})
}

func (s *Server) writeProwRuns(w http.ResponseWriter, r *http.Request, runs []prow.Run, include func(prow.SyncState) bool) {
	syncs, err := s.db.ListProwSyncs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := prowRunsResponse{Runs: runs}
	for _, st := range syncs {
		if !include(st) {
			continue
		}
		if resp.LastSuccessfulSync == nil || st.LastSuccessfulSync.Before(*resp.LastSuccessfulSync) {
			resp.LastSuccessfulSync = &st.LastSuccessfulSync
		}
		if time.Since(st.LastSuccessfulSync) > 2*st.Interval {
			resp.Stale = true
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func prowPage(r *http.Request) (limit, offset int) {
	q := r.URL.Query()
	limit, _ = strconv.Atoi(q.Get("limit"))
	offset, _ = strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	return min(limit, 200), offset
}
