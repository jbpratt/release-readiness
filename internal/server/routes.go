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

	// Snapshots API
	mux.HandleFunc("GET /api/v1/snapshots", s.handleListSnapshots)

	// Releases API (version-centric)
	mux.HandleFunc("GET /api/v1/releases/overview", s.handleReleasesOverview)
	mux.HandleFunc("GET /api/v1/releases/{version}", s.handleGetRelease)
	mux.HandleFunc("GET /api/v1/releases/{version}/snapshot", s.handleGetReleaseSnapshot)
	mux.HandleFunc("GET /api/v1/releases/{version}/issues", s.handleListReleaseIssues)
	mux.HandleFunc("GET /api/v1/releases/{version}/issues/summary", s.handleGetReleaseIssueSummary)
	mux.HandleFunc("GET /api/v1/releases/{version}/readiness", s.handleGetReleaseReadiness)

	// SPA — serve React app from embedded dist/
	distSub, _ := fs.Sub(web.DistFS, "dist")
	fileServer := http.FileServer(http.FS(distSub))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Serve static assets directly if they exist
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := distSub.Open(path); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve index.html for all other routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
