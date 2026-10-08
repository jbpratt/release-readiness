package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/quay/release-readiness/internal/db"
)

type Server struct {
	db          *db.DB
	http        *http.Server
	logger      *slog.Logger
	jiraBaseURL string
	jiraProject string
	artBaseURL  string
}

// New builds the server. An empty artBaseURL leaves every component's art null.
func New(database *db.DB, addr, jiraBaseURL, jiraProject, artBaseURL string, logger *slog.Logger) *Server {
	s := &Server{db: database, logger: logger, jiraBaseURL: jiraBaseURL, jiraProject: jiraProject, artBaseURL: artBaseURL}
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
