package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/jira"
	"github.com/quay/release-readiness/internal/kube"
	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/server"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "dashboard.db", "SQLite database path")

	// Konflux flags
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "kubeconfig path (empty uses the in-cluster service account)")
	namespace := flag.String("namespace", envOrDefault("KONFLUX_NAMESPACE", "art-quay-tenant"), "Konflux namespace to read Snapshots and Releases from")
	konfluxPollInterval := flag.Duration("konflux-poll-interval", 30*time.Second, "Konflux sync poll interval")

	// JIRA flags
	jiraURL := flag.String("jira-url", envOrDefault("JIRA_URL", "https://redhat.atlassian.net"), "JIRA Cloud URL")
	jiraEmail := flag.String("jira-email", os.Getenv("JIRA_EMAIL"), "JIRA Cloud account email for API token auth")
	jiraToken := flag.String("jira-token", os.Getenv("JIRA_TOKEN"), "JIRA Cloud API token")
	jiraProject := flag.String("jira-project", envOrDefault("JIRA_PROJECT", "PROJQUAY"), "JIRA project key")
	jiraQAContactField := flag.String("jira-qa-contact-field", envOrDefault("JIRA_QA_CONTACT_FIELD", "customfield_12315948"), "JIRA custom field name for QA Contact")
	jiraPollInterval := flag.Duration("jira-poll-interval", 5*time.Minute, "JIRA sync poll interval")

	// ART build history flags
	artURL := flag.String("art-build-history-url", "https://art-build-history-art-build-history.apps.artc2023.pc3z.p1.openshiftapps.com", "ART build history service URL (empty disables ART build links)")

	// Red Hat container catalog flags
	catalogURL := flag.String("catalog-url", "https://catalog.redhat.com/api/containers/v1", "Red Hat container catalog API URL (empty leaves JIRA's released flag as the only shipped signal)")

	// Prow flags
	prowJobs := flag.String("prow-jobs", os.Getenv("PROW_JOBS"), "periodic Prow jobs to ingest, as job_name=release_version[,...]")
	prowRehearsals := flag.String("prow-rehearsals", "", "rehearsal GCS prefixes to ingest, as gcs-prefix=release_version[,...]")
	prowInterval := flag.Duration("prow-interval", 15*time.Minute, "Prow sync poll interval")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	periodics, err := prow.ParseJobs(*prowJobs, prow.KindPeriodic)
	if err != nil {
		logger.Error("parse -prow-jobs", "error", err)
		os.Exit(1)
	}
	rehearsals, err := prow.ParseJobs(*prowRehearsals, prow.KindRehearsal)
	if err != nil {
		logger.Error("parse -prow-rehearsals", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	var wg sync.WaitGroup
	status := syncstatus.New()

	if kc, err := kube.NewClient(*kubeconfig); err != nil {
		logger.Error("create kubernetes client, konflux sync disabled", "error", err)
		status.Track("konflux", 0).Report(fmt.Errorf("create kubernetes client: %w", err))
	} else {
		konfluxLog := logger.With("component", "konflux-sync")
		logger.Info("konflux sync enabled", "namespace", *namespace, "interval", *konfluxPollInterval)
		konfluxTx := func(ctx context.Context, fn func(kube.Store) error) error {
			return database.InTx(ctx, func(txDB *db.DB) error {
				return fn(txDB)
			})
		}
		syncer := kube.NewSyncer(kc, *namespace, database, konfluxTx, konfluxLog)
		syncer.Status = status.Track("konflux", *konfluxPollInterval)
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
		syncer.Status = status.Track("jira", *jiraPollInterval)
		wg.Add(1)
		go func() {
			defer wg.Done()
			syncer.Run(ctx, *jiraPollInterval)
		}()
	}

	if *artURL != "" {
		artLog := logger.With("component", "art-resolve")
		logger.Info("art build history enabled", "url", *artURL)
		resolver := artbuild.NewResolver(artbuild.NewClient(*artURL, &http.Client{Timeout: time.Minute}), database, artLog)
		resolver.Status = status.Track("art-builds", 5*time.Minute)
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolver.Run(ctx, 5*time.Minute)
		}()
	}

	var shipped *catalog.Shipped
	if *catalogURL != "" {
		logger.Info("catalog shipped lookup enabled", "url", *catalogURL)
		shipped = catalog.NewShipped(catalog.NewClient(*catalogURL, &http.Client{Timeout: time.Minute}), logger.With("component", "catalog"))
		shipped.Status = status.Track("catalog", time.Hour)
		wg.Add(1)
		go func() {
			defer wg.Done()
			shipped.Run(ctx, time.Hour)
		}()
	}

	if prowJobList := append(periodics, rehearsals...); len(prowJobList) > 0 {
		logger.Info("prow sync enabled", "jobs", len(prowJobList), "interval", *prowInterval)
		syncer := prow.NewSyncer(prow.NewClient(prow.DefaultBaseURL), database, prowJobList, *prowInterval, logger.With("component", "prow-sync"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			syncer.Run(ctx)
		}()
	}

	srv := server.New(database, *addr, *jiraURL, *jiraProject, *artURL, shipped, status, logger)
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
