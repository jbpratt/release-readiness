package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/quay/release-readiness/internal/artfbc"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/release"
	"github.com/quay/release-readiness/internal/syncstatus"
)

type Server struct {
	db          *db.DB
	http        *http.Server
	logger      *slog.Logger
	jiraBaseURL string
	jiraProject string
	selector    *release.Selector
	syncStatus  *syncstatus.Registry
	// KonfluxUIURL and KonfluxNamespace locate the Konflux UI pages the web
	// links to; an empty URL links none.
	KonfluxUIURL, KonfluxNamespace string
	// ArtFBC looks up the tags ART publishes its FBC images under; nil turns
	// the lookup off.
	ArtFBC *artfbc.Client
}

// New builds the server.
func New(database *db.DB, addr, jiraBaseURL, jiraProject string, selector *release.Selector, syncStatus *syncstatus.Registry, logger *slog.Logger) *Server {
	s := &Server{db: database, logger: logger, jiraBaseURL: jiraBaseURL, jiraProject: jiraProject, selector: selector, syncStatus: syncStatus}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	var handler http.Handler = mux
	handler = loggingMiddleware(logger, handler)
	handler = recoveryMiddleware(logger, handler)

	s.http = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Run(ctx context.Context) error {
	// Buffered so the goroutine never blocks once Run has stopped receiving.
	errc := make(chan error, 1)
	go func() {
		s.logger.Info("listening", "addr", s.http.Addr)
		errc <- s.http.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}
	s.logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
