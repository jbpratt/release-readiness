package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
)

// quayVersion is a concrete version, named after its ART assembly:
// quay-v3.18.1 is assembly 3.18.1.
var quayVersion = regexp.MustCompile(`^quay-v(\d+\.\d+\.\d+)$`)

// handleGetCandidate serves what decides whether an unshipped version can
// ship: its operator FBCs up for release and as its stream built them, the
// version's FBC catalogs, and its STAGE build's prod Release and the images
// that build leaves out.
func (s *Server) handleGetCandidate(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	resp, _, err := s.selector.ReleaseCandidate(r.Context(), release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CandidateDigests returns the selector's CandidateDigests.
func (s *Server) CandidateDigests(ctx context.Context) ([]string, error) {
	return s.selector.CandidateDigests(ctx)
}
