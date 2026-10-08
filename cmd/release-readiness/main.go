package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/jira"
	"github.com/quay/release-readiness/internal/kube"
	"github.com/quay/release-readiness/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "dashboard.db", "SQLite database path")

	// Konflux flags
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "kubeconfig path (empty uses the in-cluster service account)")
	namespace := flag.String("namespace", envOrDefault("KONFLUX_NAMESPACE", "art-quay-tenant"), "Konflux namespace to read Snapshots from")
	konfluxPollInterval := flag.Duration("konflux-poll-interval", 30*time.Second, "Konflux sync poll interval")

	// JIRA flags
	jiraURL := flag.String("jira-url", envOrDefault("JIRA_URL", "https://redhat.atlassian.net"), "JIRA Cloud URL")
	jiraEmail := flag.String("jira-email", os.Getenv("JIRA_EMAIL"), "JIRA Cloud account email for API token auth")
	jiraToken := flag.String("jira-token", os.Getenv("JIRA_TOKEN"), "JIRA Cloud API token")
	jiraProject := flag.String("jira-project", envOrDefault("JIRA_PROJECT", "PROJQUAY"), "JIRA project key")
	jiraQAContactField := flag.String("jira-qa-contact-field", envOrDefault("JIRA_QA_CONTACT_FIELD", "customfield_12315948"), "JIRA custom field name for QA Contact")
	jiraPollInterval := flag.Duration("jira-poll-interval", 5*time.Minute, "JIRA sync poll interval")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	var wg sync.WaitGroup

	if kc, err := kube.NewClient(*kubeconfig); err != nil {
		logger.Error("create kubernetes client, konflux sync disabled", "error", err)
	} else {
		konfluxLog := logger.With("component", "konflux-sync")
		logger.Info("konflux sync enabled", "namespace", *namespace, "interval", *konfluxPollInterval)
		konfluxTx := func(ctx context.Context, fn func(kube.Store) error) error {
			return database.InTx(ctx, func(txDB *db.DB) error {
				return fn(txDB)
			})
		}
		syncer := kube.NewSyncer(kc, *namespace, database, konfluxTx, konfluxLog)
		wg.Add(1)
		go func() {
			defer wg.Done()
			syncer.Run(ctx, *konfluxPollInterval)
		}()
	}

	// Start JIRA sync if token is configured
	if *jiraToken != "" {
		jiraClient := jira.New(jira.Config{
			BaseURL:        *jiraURL,
			Email:          *jiraEmail,
			Token:          *jiraToken,
			Project:        *jiraProject,
			QAContactField: *jiraQAContactField,
		})
		jiraLog := logger.With("component", "jira-sync")
		logger.Info("jira sync enabled", "url", *jiraURL, "project", *jiraProject, "interval", *jiraPollInterval)
		jiraTx := func(ctx context.Context, fn func(jira.Store) error) error {
			return database.InTx(ctx, func(txDB *db.DB) error {
				return fn(txDB)
			})
		}
		syncer := jira.NewSyncer(jiraClient, database, jiraTx, jiraLog)
		wg.Add(1)
		go func() {
			defer wg.Done()
			syncer.Run(ctx, *jiraPollInterval)
		}()
	}

	srv := server.New(database, *addr, *jiraURL, *jiraProject, logger)
	if err := srv.Run(ctx); err != nil {
		logger.Error("server", "error", err)
		os.Exit(1)
	}

	wg.Wait()
	logger.Info("all background tasks stopped")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
