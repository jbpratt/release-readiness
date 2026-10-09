package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/github"
	"github.com/quay/release-readiness/internal/syncstatus"
)

type Server struct {
	db          *db.DB
	http        *http.Server
	logger      *slog.Logger
	jiraBaseURL string
	jiraProject string
	artBaseURL  string
	shipped     *catalog.Shipped
	syncStatus  *syncstatus.Registry
	// StageReleasePlanPattern matches the ReleasePlan names whose Releases
	// are image STAGE; nil selects no build.
	StageReleasePlanPattern *regexp.Regexp
	// Scanner holds the GitHub compares of the selected STAGE builds; nil
	// has compared nothing.
	Scanner *github.Scanner
}

// New builds the server. An empty artBaseURL leaves every component's art null;
// a nil shipped leaves JIRA's released flag as the only shipped signal.
func New(database *db.DB, addr, jiraBaseURL, jiraProject, artBaseURL string, shipped *catalog.Shipped, syncStatus *syncstatus.Registry, logger *slog.Logger) *Server {
	s := &Server{db: database, logger: logger, jiraBaseURL: jiraBaseURL, jiraProject: jiraProject, artBaseURL: artBaseURL, shipped: shipped, syncStatus: syncStatus}
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
	go func() {
		s.logger.Info("listening", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("server error", "error", err)
		}
	}()

	<-ctx.Done()
	s.logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
