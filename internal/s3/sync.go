package s3

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/clair"
	"github.com/quay/release-readiness/internal/ctrf"
	"github.com/quay/release-readiness/internal/model"
)

// Store is the subset of the database layer needed by the S3 syncer.
type Store interface {
	SnapshotExistsByName(ctx context.Context, name string) (bool, error)
	CreateSnapshotWithComponents(ctx context.Context, snap model.Snapshot, testsPassed bool, createdAt time.Time) (*model.SnapshotRecord, error)
	CreateTestSuite(ctx context.Context, snapshotID int64, name, status, pipelineRun, toolName, toolVersion string, tests, passed, failed, skipped, pending, other, flaky int, startTime, stopTime, durationMs int64) (int64, error)
	CreateTestCase(ctx context.Context, testSuiteID int64, name, status string, durationMs float64, message, trace, filePath, suite string, retries int, flaky bool) error
	CreateVulnerabilityReport(ctx context.Context, snapshotID int64, component, arch string, total, critical, high, medium, low, unknown, fixable int) (int64, error)
	CreateVulnerability(ctx context.Context, reportID int64, name, severity, packageName, packageVersion, fixedInVersion, description, link string) error
}

// TxFunc wraps a function in a database transaction, passing a tx-scoped Store.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Syncer orchestrates periodic S3 snapshot synchronisation into a Store.
type Syncer struct {
	client *Client
	store  Store
	withTx TxFunc
	logger *slog.Logger
}

// NewSyncer creates a Syncer that uses client to fetch data and store to persist it.
func NewSyncer(client *Client, store Store, withTx TxFunc, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, store: store, withTx: withTx, logger: logger}
}

// Run performs an immediate sync and then repeats every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	s.SyncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

// SyncOnce discovers all applications and ingests any new snapshots.
func (s *Syncer) SyncOnce(ctx context.Context) {
	apps, err := s.client.ListApplications(ctx)
	if err != nil {
		s.logger.Error("list applications", "error", err)
		return
	}

	for _, app := range apps {
		keys, err := s.client.ListSnapshots(ctx, app)
		if err != nil {
			s.logger.Error("list snapshots", "application", app, "error", err)
			continue
		}

		for _, key := range keys {
			snap, err := s.client.GetSnapshot(ctx, key)
			if err != nil {
				s.logger.Debug("skipping snapshot", "key", key, "error", err)
				continue
			}

			exists, err := s.store.SnapshotExistsByName(ctx, snap.Snapshot)
			if err != nil {
				s.logger.Error("check snapshot", "snapshot", snap.Snapshot, "error", err)
				continue
			}
			if exists {
				continue
			}

			s.logger.Info("new snapshot", "snapshot", snap.Snapshot, "application", app)

			if err := s.withTx(ctx, func(txStore Store) error {
				txSyncer := &Syncer{client: s.client, store: txStore, withTx: s.withTx, logger: s.logger}
				return txSyncer.ingest(ctx, key, snap)
			}); err != nil {
				s.logger.Error("ingest snapshot", "snapshot", snap.Snapshot, "error", err)
			}
		}
	}
}

type suiteData struct {
	name   string
	report *ctrf.Report
}

// ingest persists a single snapshot and its components/test results into the store.
func (s *Syncer) ingest(ctx context.Context, key string, snap *model.Snapshot) error {
	// Derive the snapshot directory prefix from the key.
	// key is like "{app}/snapshots/{snapshot-name}/snapshot.json"
	snapshotDir := path.Dir(key) + "/"

	// Discover test suites from S3 and fetch CTRF reports to determine testsPassed.
	suiteNames, err := s.client.ListTestSuites(ctx, snapshotDir)
	if err != nil {
		s.logger.Debug("no test suites found", "snapshot", snap.Snapshot, "error", err)
	}

	var suites []suiteData
	testsPassed := len(suiteNames) > 0
	for _, name := range suiteNames {
		ctrfPath := snapshotDir + name + "/results/ctrf-report.json"
		report, err := s.client.GetCTRFReport(ctx, ctrfPath)
		if err != nil {
			s.logger.Debug("failed to fetch ctrf report", "suite", name, "error", err)
			continue
		}
		suites = append(suites, suiteData{name: name, report: report})
		if report.Results.Summary.Failed > 0 {
			testsPassed = false
		}
	}
	if len(suites) == 0 {
		testsPassed = false
	}

	snapshotRecord, err := s.store.CreateSnapshotWithComponents(ctx, *snap, testsPassed, time.Now().UTC())
	if err != nil {
		return err
	}

	for _, sd := range suites {
		status := "passed"
		if sd.report.Results.Summary.Failed > 0 {
			status = "failed"
		}

		sum := sd.report.Results.Summary
		suiteID, err := s.store.CreateTestSuite(
			ctx, snapshotRecord.ID,
			sd.name, status, "",
			sd.report.Results.Tool.Name, sd.report.Results.Tool.Version,
			sum.Tests, sum.Passed, sum.Failed, sum.Skipped,
			sum.Pending, sum.Other, sum.Flaky,
			sum.Start, sum.Stop, sum.Stop-sum.Start,
		)
		if err != nil {
			return fmt.Errorf("create test suite %s: %w", sd.name, err)
		}

		for _, tc := range sd.report.Results.Tests {
			if err := s.store.CreateTestCase(
				ctx, suiteID,
				tc.Name, tc.Status, tc.Duration,
				tc.Message, tc.Trace, tc.FilePath, tc.Suite,
				tc.Retries, tc.Flaky,
			); err != nil {
				return fmt.Errorf("create test case %s: %w", tc.Name, err)
			}
		}
	}

	// Ingest Clair vulnerability scans.
	if err := s.ingestScans(ctx, snapshotDir, snapshotRecord.ID); err != nil {
		s.logger.Error("ingest scans", "snapshot", snap.Snapshot, "error", err)
	}

	return nil
}

// ingestScans fetches scan summary and clair reports from S3, persisting vulnerability data.
func (s *Syncer) ingestScans(ctx context.Context, snapshotDir string, snapshotID int64) error {
	summary, err := s.client.GetScanSummary(ctx, snapshotDir)
	if err != nil {
		return nil // scans directory may not exist
	}

	for _, entry := range summary {
		if entry.Status != "ok" {
			continue
		}

		reportKeys, err := s.client.ListClairReports(ctx, snapshotDir, entry.Component)
		if err != nil {
			s.logger.Debug("list clair reports", "component", entry.Component, "error", err)
			continue
		}

		for _, key := range reportKeys {
			arch := archFromKey(key)
			report, err := s.client.GetClairReport(ctx, key)
			if err != nil {
				s.logger.Debug("fetch clair report", "key", key, "error", err)
				continue
			}

			counts := countSeverities(report)
			reportID, err := s.store.CreateVulnerabilityReport(
				ctx, snapshotID, entry.Component, arch,
				counts.total, counts.critical, counts.high,
				counts.medium, counts.low, counts.unknown, counts.fixable,
			)
			if err != nil {
				return fmt.Errorf("create vulnerability report %s/%s: %w", entry.Component, arch, err)
			}

			for _, v := range report.Vulnerabilities {
				link := firstLink(v.Links)
				if err := s.store.CreateVulnerability(
					ctx, reportID,
					v.Name, v.NormalizedSeverity,
					v.Package.Name, "",
					v.FixedInVersion, v.Description, link,
				); err != nil {
					return fmt.Errorf("create vulnerability %s: %w", v.Name, err)
				}
			}

			s.logger.Info("ingested clair report",
				"component", entry.Component, "arch", arch,
				"vulnerabilities", counts.total)
		}
	}
	return nil
}

type severityCounts struct {
	total, critical, high, medium, low, unknown, fixable int
}

func countSeverities(report *clair.Report) severityCounts {
	var c severityCounts
	for _, v := range report.Vulnerabilities {
		c.total++
		switch v.NormalizedSeverity {
		case "Critical":
			c.critical++
		case "High":
			c.high++
		case "Medium":
			c.medium++
		case "Low":
			c.low++
		default:
			c.unknown++
		}
		if v.FixedInVersion != "" {
			c.fixable++
		}
	}
	return c
}

// archFromKey extracts the architecture from a key like ".../clair-report-amd64.json".
func archFromKey(key string) string {
	base := path.Base(key)                           // clair-report-amd64.json
	base = strings.TrimPrefix(base, "clair-report-") // amd64.json
	base = strings.TrimSuffix(base, ".json")         // amd64
	return base
}

// firstLink returns the first whitespace-separated link from a Clair links string.
func firstLink(links string) string {
	if i := strings.IndexByte(links, ' '); i > 0 {
		return links[:i]
	}
	return links
}
