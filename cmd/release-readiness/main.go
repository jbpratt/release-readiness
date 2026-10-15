package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/transport"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/jira"
	"github.com/quay/release-readiness/internal/kube"
	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/reconcile"
	"github.com/quay/release-readiness/internal/scan"
	"github.com/quay/release-readiness/internal/server"
	"github.com/quay/release-readiness/internal/shipment"
	"github.com/quay/release-readiness/internal/syncstatus"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "dashboard.db", "SQLite database path")

	// Konflux flags
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "kubeconfig path (empty uses the in-cluster service account)")
	namespace := flag.String("namespace", cmp.Or(os.Getenv("KONFLUX_NAMESPACE"), "art-quay-tenant"), "Konflux namespace to read Snapshots and Releases from")
	konfluxPollInterval := flag.Duration("konflux-poll-interval", 30*time.Second, "Konflux sync poll interval")
	konfluxUIURL := flag.String("konflux-ui-url", cmp.Or(os.Getenv("KONFLUX_UI_URL"), "https://konflux-ui.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com"), "Konflux UI URL for links to -namespace's applications, Snapshots and Releases (empty disables Konflux links)")

	// JIRA flags
	jiraURL := flag.String("jira-url", cmp.Or(os.Getenv("JIRA_URL"), "https://redhat.atlassian.net"), "JIRA Cloud URL")
	jiraAPIURL := flag.String("jira-api-url", os.Getenv("JIRA_API_URL"), "JIRA REST API base URL (defaults to -jira-url; use https://api.atlassian.com/ex/jira/<cloudId> for scoped API tokens)")
	jiraEmail := flag.String("jira-email", os.Getenv("JIRA_EMAIL"), "JIRA Cloud account email for API token auth")
	jiraToken := flag.String("jira-token", os.Getenv("JIRA_TOKEN"), "JIRA Cloud API token")
	jiraProject := flag.String("jira-project", cmp.Or(os.Getenv("JIRA_PROJECT"), "PROJQUAY"), "JIRA project key")
	jiraPollInterval := flag.Duration("jira-poll-interval", 5*time.Minute, "JIRA sync poll interval")

	// ART build history flags
	artURL := flag.String("art-build-history-url", "https://art-build-history-art-build-history.apps.artc2023.pc3z.p1.openshiftapps.com", "ART build history service URL (empty disables ART build links)")

	// Tekton Results flags
	resultsURL := flag.String("tekton-results-url", "https://tekton-results-tekton-results.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com", "Tekton Results API URL to read the CVE scans of candidate images from with the Konflux credential (empty disables)")

	// Red Hat container catalog flags
	catalogURL := flag.String("catalog-url", "https://catalog.redhat.com/api/containers/v1", "Red Hat container catalog API URL (empty leaves JIRA's released flag as the only shipped signal)")

	// Prow flags
	prowJobs := flag.String("prow-jobs", os.Getenv("PROW_JOBS"), "periodic Prow jobs to ingest, as job_name=konflux_application[,...] (e.g. ...-aws-s3-nightly=quay-3-18)")
	prowJobPattern := flag.String("prow-job-pattern", os.Getenv("PROW_JOB_PATTERN"), "^-anchored regexp of periodic Prow job names to discover; its named groups major and minor give the application quay-<major>-<minor>, and a -prow-jobs entry overrides it")
	prowInterval := flag.Duration("prow-interval", 15*time.Minute, "Prow sync poll interval")

	// GitLab shipment MR flags
	gitlabURL := flag.String("gitlab-url", "https://gitlab.cee.redhat.com", "GitLab URL to read ART shipment MRs from (empty disables)")
	gitlabToken := flag.String("gitlab-token", os.Getenv("GITLAB_TOKEN"), "GitLab read_api token (empty reads anonymously, throttled)")
	shipmentProject := flag.Int("shipment-project", 116177, "GitLab project ID of ART's shipment MRs (ocp-shipment-data)")
	buildDataURL := flag.String("ocp-build-data-url", "https://raw.githubusercontent.com/openshift-eng/ocp-build-data", "raw ocp-build-data URL, whose quay-X.Y releases.yml names each assembly's shipment MR")
	gitlabPollInterval := flag.Duration("gitlab-poll-interval", 5*time.Minute, "GitLab shipment MR poll interval")

	// Selected STAGE build flags
	stagePlanPattern := flag.String("stage-release-plan-pattern", cmp.Or(os.Getenv("STAGE_RELEASE_PLAN_PATTERN"), `^quay-advisory-stage-\d+-\d+$`), "regexp matching the Konflux ReleasePlan names whose Releases are image STAGE (empty selects no staged build)")

	// Retention flags
	retention := flag.Duration("retention", 720*time.Hour, "age past which data no release needs is pruned daily and not ingested (0 disables)")

	// GitHub flags
	githubToken := flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "read-only GitHub token for commit range lookups")

	// Jira writer flags
	jiraWrite := flag.Bool("jira-write", os.Getenv("JIRA_WRITE") == "true", "write the reconciler's proposed actions to JIRA (needs -jira-write-email and -jira-write-token)")
	jiraWriteEmail := flag.String("jira-write-email", os.Getenv("JIRA_WRITE_EMAIL"), "JIRA Cloud account email of the write credential")
	jiraWriteToken := flag.String("jira-write-token", os.Getenv("JIRA_WRITE_TOKEN"), "JIRA Cloud API token of the write credential, never the -jira-token one")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	prowJobList, err := prow.ParseJobs(*prowJobs)
	if err != nil {
		logger.Error("parse -prow-jobs", "error", err)
		os.Exit(1)
	}
	var prowPattern *prow.JobPattern
	if *prowJobPattern != "" {
		if prowPattern, err = prow.ParseJobPattern(*prowJobPattern); err != nil {
			logger.Error("parse -prow-job-pattern", "error", err)
			os.Exit(1)
		}
	}
	var stagePlans *regexp.Regexp
	if *stagePlanPattern != "" {
		if stagePlans, err = regexp.Compile(*stagePlanPattern); err != nil {
			logger.Error("parse -stage-release-plan-pattern", "error", err)
			os.Exit(1)
		}
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
	lastSuccess, err := database.SyncStatus(ctx)
	if err != nil {
		logger.Warn("load sync status, last success starts empty", "error", err)
	}
	status.Persist(lastSuccess, func(source string, at time.Time) {
		if err := database.UpsertSyncStatus(ctx, source, at); err != nil {
			logger.Warn("save sync status", "source", source, "error", err)
		}
	})

	kubeCfg, err := kube.RESTConfig(*kubeconfig)
	var kc *dynamic.DynamicClient
	if err == nil {
		kc, err = dynamic.NewForConfig(kubeCfg)
	}
	if err != nil {
		logger.Error("create kubernetes client, konflux sync disabled", "error", err)
		status.Track("konflux", 0).Report(fmt.Errorf("create kubernetes client: %w", err))
	} else {
		konfluxLog := logger.With("component", "konflux-sync")
		logger.Info("konflux sync enabled", "namespace", *namespace, "interval", *konfluxPollInterval)
		syncer := kube.NewSyncer(kc, *namespace, database, konfluxLog)
		syncer.Status = status.Track("konflux", *konfluxPollInterval)
		syncer.Retention = *retention
		wg.Go(func() { syncer.Run(ctx, *konfluxPollInterval) })
	}

	// Start JIRA sync if token is configured
	if *jiraToken != "" {
		apiURL := cmp.Or(*jiraAPIURL, *jiraURL)
		jiraClient := jira.New(jira.Config{
			BaseURL: apiURL,
			SiteURL: *jiraURL,
			Email:   *jiraEmail,
			Token:   *jiraToken,
			Project: *jiraProject,
		})
		jiraLog := logger.With("component", "jira-sync")
		logger.Info("jira sync enabled", "url", *jiraURL, "api_url", apiURL, "project", *jiraProject, "interval", *jiraPollInterval)
		jiraTx := func(ctx context.Context, fn func(jira.Store) error) error {
			return database.InTx(ctx, func(txDB *db.DB) error {
				return fn(txDB)
			})
		}
		syncer := jira.NewSyncer(jiraClient, database, jiraTx, jiraLog)
		syncer.Status = status.Track("jira", *jiraPollInterval)
		wg.Go(func() { syncer.Run(ctx, *jiraPollInterval) })
	}

	var art *artbuild.Client
	if *artURL != "" {
		artLog := logger.With("component", "art-resolve")
		logger.Info("art build history enabled", "url", *artURL)
		art = artbuild.NewClient(*artURL, &http.Client{Timeout: time.Minute})
		resolver := artbuild.NewResolver(art, database, artLog)
		resolver.Status = status.Track("art-builds", 5*time.Minute)
		wg.Go(func() { resolver.Run(ctx, 5*time.Minute) })
		history := artbuild.NewHistorySync(art, database, logger.With("component", "art-history"))
		history.Status = status.Track("art-history", 10*time.Minute)
		wg.Go(func() { history.Run(ctx, 10*time.Minute) })
		render := fbc.NewSync(fbc.NewRegistry(&http.Client{Timeout: 5 * time.Minute}), database, history, logger.With("component", "fbc-render"))
		render.Status = status.Track("fbc-render", 10*time.Minute)
		wg.Go(func() { render.Run(ctx, 10*time.Minute) })

		if *githubToken == "" {
			logger.Warn("no -github-token, reconcile reads GitHub unauthenticated")
		}
		commits := reconcile.NewGitHubFetcher(&http.Client{Timeout: time.Minute}, *githubToken)
		reconciler := reconcile.NewSync(database, commits, logger.With("component", "reconcile"))
		reconciler.Tx = func(ctx context.Context, fn func(reconcile.Store) error) error {
			return database.InTx(ctx, func(txDB *db.DB) error {
				return fn(txDB)
			})
		}
		reconciler.Status = status.Track("reconcile", 10*time.Minute)
		switch {
		case !*jiraWrite:
			logger.Info("jira writer off; proposed actions are logged only")
		case *jiraWriteEmail == "" || *jiraWriteToken == "":
			logger.Error("-jira-write without -jira-write-email and -jira-write-token, jira writer disabled")
		default:
			apiURL := cmp.Or(*jiraAPIURL, *jiraURL)
			logger.Info("jira writer enabled", "api_url", apiURL)
			writeClient := jira.New(jira.Config{BaseURL: apiURL, Email: *jiraWriteEmail, Token: *jiraWriteToken, Project: *jiraProject})
			reconciler.Writer = reconcile.NewWriter(writeClient, database, *jiraProject, logger.With("component", "jira-writer"))
		}
		wg.Go(func() { reconciler.Run(ctx, 10*time.Minute) })
	}

	var shipped *catalog.Shipped
	if *catalogURL != "" {
		logger.Info("catalog shipped lookup enabled", "url", *catalogURL)
		shipped = catalog.NewShipped(catalog.NewClient(*catalogURL, &http.Client{Timeout: time.Minute}), logger.With("component", "catalog"))
		shipped.Status = status.Track("catalog", time.Hour)
		wg.Go(func() { shipped.Run(ctx, time.Hour) })
	}

	if len(prowJobList) > 0 || prowPattern != nil {
		logger.Info("prow sync enabled", "jobs", len(prowJobList), "pattern", *prowJobPattern, "interval", *prowInterval)
		syncer := prow.NewSyncer(prow.NewClient(prow.DefaultBaseURL), database, prowJobList, *prowInterval, logger.With("component", "prow-sync"))
		syncer.Status = status.Track("prow", *prowInterval)
		syncer.Retention = *retention
		syncer.Pattern = prowPattern
		wg.Go(func() { syncer.Run(ctx) })
	}

	if *retention > 0 {
		logger.Info("retention enabled", "retention", *retention)
		retentionStatus := status.Track("retention", 24*time.Hour)
		wg.Go(func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				deleted, err := database.Prune(ctx, time.Now().Add(-*retention))
				if err != nil {
					logger.Error("prune", "component", "retention", "deleted", deleted, "error", err)
				} else {
					logger.Info("pruned", "component", "retention", "deleted", deleted)
				}
				retentionStatus.Report(err)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}

	srv := server.New(database, *addr, *jiraURL, *jiraProject, *artURL, shipped, status, logger)
	srv.StageReleasePlanPattern = stagePlans
	srv.KonfluxUIURL = *konfluxUIURL
	srv.KonfluxNamespace = *namespace

	if *gitlabURL != "" {
		logger.Info("shipment MR sync enabled", "url", *gitlabURL, "project", *shipmentProject, "token", *gitlabToken != "", "interval", *gitlabPollInterval)
		client := shipment.NewClient(*gitlabURL, *buildDataURL, *shipmentProject, *gitlabToken, &http.Client{Timeout: time.Minute})
		syncer := shipment.NewSyncer(client, database, srv.ShipmentVersions, logger.With("component", "shipment-mrs"))
		syncer.Status = status.Track("shipment-mrs", *gitlabPollInterval)
		wg.Go(func() { syncer.Run(ctx, *gitlabPollInterval) })
	}

	if *resultsURL != "" {
		var rt http.RoundTripper
		switch {
		case kc == nil:
			err = errors.New("no Konflux client")
		case *artURL == "":
			err = errors.New("ART build history disabled")
		case kubeCfg.BearerToken == "" && kubeCfg.BearerTokenFile == "":
			err = errors.New("the Konflux credential has no bearer token")
		default:
			// The system roots, not the cluster CA, verify the Results route.
			rt, err = transport.NewBearerAuthWithRefreshRoundTripper(kubeCfg.BearerToken, kubeCfg.BearerTokenFile, http.DefaultTransport.(*http.Transport).Clone())
		}
		if err != nil {
			logger.Warn("image scan sync disabled", "error", err)
		} else {
			logger.Info("image scan sync enabled", "url", *resultsURL)
			results := scan.NewClient(*resultsURL, &http.Client{Timeout: time.Minute, Transport: rt})
			syncer := scan.NewSyncer(results, art, database, srv.CandidateDigests, logger.With("component", "image-scans"))
			syncer.Status = status.Track("image-scans", 5*time.Minute)
			wg.Go(func() {
				// Until the catalog is read, the versions it shipped look
				// unshipped, and their scans would be read.
				if shipped != nil {
					shipped.Refresh(ctx)
				}
				syncer.Run(ctx, 5*time.Minute)
			})
		}
	}

	if err := srv.Run(ctx); err != nil {
		logger.Error("server", "error", err)
		os.Exit(1)
	}

	wg.Wait()
	logger.Info("all background tasks stopped")
}
