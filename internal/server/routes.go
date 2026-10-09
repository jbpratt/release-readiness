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
	mux.HandleFunc("GET /api/v1/releases/{version}/snapshots", s.handleListReleaseSnapshots)
	mux.HandleFunc("GET /api/v1/releases/{version}/snapshots/{name}", s.handleGetReleaseSnapshot)
	mux.HandleFunc("GET /api/v1/releases/{version}/snapshots/{name}/prow-runs", s.handleListSnapshotProwRuns)
	mux.HandleFunc("GET /api/v1/releases/{version}/issues", s.handleListReleaseIssues)
	mux.HandleFunc("GET /api/v1/releases/{version}/issues/summary", s.handleGetReleaseIssueSummary)
	mux.HandleFunc("GET /api/v1/releases/{version}/readiness", s.handleGetReleaseReadiness)
	mux.HandleFunc("GET /api/v1/releases/{version}/prow-runs", s.handleListReleaseProwRuns)
	mux.HandleFunc("GET /api/v1/releases/{version}/build-attempts", s.handleListBuildAttempts)
	mux.HandleFunc("GET /api/v1/releases/{version}/staged", s.handleGetStaged)
	mux.HandleFunc("GET /api/v1/releases/{version}/build-tickets", s.handleGetBuildTickets)

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
