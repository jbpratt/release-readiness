package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/quay/release-readiness/web"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health & Config
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/config", s.handleConfig)
	mux.HandleFunc("GET /api/v1/sync-status", s.handleSyncStatus)

	// Releases API (version-centric)
	mux.HandleFunc("GET /api/v1/releases/overview", s.handleReleasesOverview)
	mux.HandleFunc("GET /api/v1/releases/{version}", s.handleGetRelease)
	mux.HandleFunc("GET /api/v1/releases/{version}/issues", s.handleListReleaseIssues)
	mux.HandleFunc("GET /api/v1/releases/{version}/candidate", s.handleGetCandidate)
	mux.HandleFunc("GET /api/v1/releases/{version}/fbc-tags", s.handleGetFBCTags)
	mux.HandleFunc("GET /api/v1/releases/{version}/ci", s.handleGetReleaseCI)

	// SPA — serve React app from embedded dist/
	distSub, _ := fs.Sub(web.DistFS, "dist")
	fileServer := http.FileServer(http.FS(distSub))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for any path that is not a built file
		if _, err := fs.Stat(distSub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
