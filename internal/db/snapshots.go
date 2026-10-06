package db

import (
	"context"
	"fmt"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) CreateSnapshot(ctx context.Context, application, name string, testsPassed bool, createdAt time.Time) (*model.SnapshotRecord, error) {
	id, err := d.queries().CreateSnapshot(ctx, dbsqlc.CreateSnapshotParams{
		Application: application,
		Name:        name,
		TestsPassed: boolToInt64(testsPassed),
		CreatedAt:   createdAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return &model.SnapshotRecord{
		ID:          id,
		Application: application,
		Name:        name,
		TestsPassed: testsPassed,
		CreatedAt:   createdAt.UTC(),
	}, nil
}

// CreateSnapshotWithComponents inserts snap and its components, ensuring each
// component exists in the components table. Run it inside InTx for atomicity.
func (d *DB) CreateSnapshotWithComponents(ctx context.Context, snap model.Snapshot, testsPassed bool, createdAt time.Time) (*model.SnapshotRecord, error) {
	record, err := d.CreateSnapshot(ctx, snap.Application, snap.Snapshot, testsPassed, createdAt)
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	for _, comp := range snap.Components {
		if _, err := d.EnsureComponent(ctx, comp.Name); err != nil {
			return nil, fmt.Errorf("ensure component %s: %w", comp.Name, err)
		}
		if err := d.CreateSnapshotComponent(ctx, record.ID, comp.Name, comp.GitRevision, comp.ContainerImage, comp.GitURL); err != nil {
			return nil, fmt.Errorf("create snapshot component %s: %w", comp.Name, err)
		}
	}
	return record, nil
}

func (d *DB) SnapshotExistsByName(ctx context.Context, name string) (bool, error) {
	count, err := d.queries().SnapshotExistsByName(ctx, name)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) GetSnapshotByID(ctx context.Context, id int64) (*model.SnapshotRecord, error) {
	row, err := d.queries().GetSnapshotByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s := toSnapshotRecord(row)
	return &s, nil
}

func (d *DB) GetTestSuiteByID(ctx context.Context, id int64) (*model.TestSuiteMeta, error) {
	row, err := d.queries().GetTestSuiteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &model.TestSuiteMeta{
		ID:         row.ID,
		SnapshotID: row.SnapshotID,
		Name:       row.Name,
	}, nil
}

func (d *DB) GetSnapshotByName(ctx context.Context, name string) (*model.SnapshotRecord, error) {
	row, err := d.queries().GetSnapshotRow(ctx, name)
	if err != nil {
		return nil, err
	}
	s := toSnapshotRecord(row)

	components, err := d.listSnapshotComponents(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	s.Components = components

	suites, err := d.ListTestSuites(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	for i, suite := range suites {
		cases, err := d.ListTestCases(ctx, suite.ID)
		if err != nil {
			return nil, err
		}
		suites[i].TestCases = cases
	}
	s.TestSuites = suites
	s.HasTests = len(suites) > 0

	vulnReports, err := d.ListVulnerabilityReports(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	for i, rpt := range vulnReports {
		vulns, err := d.ListVulnerabilities(ctx, rpt.ID)
		if err != nil {
			return nil, err
		}
		vulnReports[i].Vulnerabilities = vulns
	}
	s.VulnerabilityReports = vulnReports

	return &s, nil
}

func (d *DB) CreateSnapshotComponent(ctx context.Context, snapshotID int64, component, gitSHA, imageURL, gitURL string) error {
	return d.queries().CreateSnapshotComponent(ctx, dbsqlc.CreateSnapshotComponentParams{
		SnapshotID: snapshotID,
		Component:  component,
		GitSha:     gitSHA,
		ImageUrl:   imageURL,
		GitUrl:     gitURL,
	})
}

func (d *DB) listSnapshotComponents(ctx context.Context, snapshotID int64) ([]model.ComponentRecord, error) {
	rows, err := d.queries().ListSnapshotComponents(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	components := make([]model.ComponentRecord, len(rows))
	for i, r := range rows {
		components[i] = model.ComponentRecord{
			ID:         r.ID,
			SnapshotID: r.SnapshotID,
			Component:  r.Component,
			GitSHA:     r.GitSha,
			ImageURL:   r.ImageUrl,
			GitURL:     r.GitUrl,
		}
	}
	return components, nil
}

func (d *DB) ListSnapshots(ctx context.Context, applications []string, limit, offset int) ([]model.SnapshotRecord, error) {
	var rows []dbsqlc.Snapshot
	var err error
	if len(applications) > 0 {
		rows, err = d.queries().ListSnapshotsByApplications(ctx, dbsqlc.ListSnapshotsByApplicationsParams{
			Applications: applications,
			Limit:        int64(limit),
			Offset:       int64(offset),
		})
	} else {
		rows, err = d.queries().ListAllSnapshots(ctx, dbsqlc.ListAllSnapshotsParams{
			Limit:  int64(limit),
			Offset: int64(offset),
		})
	}
	if err != nil {
		return nil, err
	}
	snapshots := make([]model.SnapshotRecord, len(rows))
	for i, r := range rows {
		snapshots[i] = toSnapshotRecord(r)
	}
	return snapshots, nil
}

func (d *DB) LatestSnapshotPerApplication(ctx context.Context) ([]model.ApplicationSummary, error) {
	rows, err := d.queries().LatestSnapshotPerApplication(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]model.ApplicationSummary, len(rows))
	for i, r := range rows {
		s := model.SnapshotRecord{
			ID:          r.ID,
			Application: r.Application,
			Name:        r.Name,
			TestsPassed: r.TestsPassed == 1,
			HasTests:    r.TestCount > 0,
			CreatedAt:   parseTime(r.CreatedAt),
		}
		summaries[i] = model.ApplicationSummary{
			Application:    r.Application,
			LatestSnapshot: &s,
			SnapshotCount:  int(r.Cnt),
		}
	}
	return summaries, nil
}

func (d *DB) CreateTestSuite(ctx context.Context, snapshotID int64, name, status, pipelineRun, toolName, toolVersion string, tests, passed, failed, skipped, pending, other, flaky int, startTime, stopTime, durationMs int64) (int64, error) {
	return d.queries().CreateTestSuite(ctx, dbsqlc.CreateTestSuiteParams{
		SnapshotID:  snapshotID,
		Name:        name,
		Status:      status,
		PipelineRun: pipelineRun,
		ToolName:    toolName,
		ToolVersion: toolVersion,
		Tests:       int64(tests),
		Passed:      int64(passed),
		Failed:      int64(failed),
		Skipped:     int64(skipped),
		Pending:     int64(pending),
		Other:       int64(other),
		Flaky:       int64(flaky),
		StartTime:   startTime,
		StopTime:    stopTime,
		DurationMs:  durationMs,
	})
}

func (d *DB) CreateTestCase(ctx context.Context, testSuiteID int64, name, status string, durationMs float64, message, trace, filePath, suite string, retries int, flaky bool) error {
	return d.queries().CreateTestCase(ctx, dbsqlc.CreateTestCaseParams{
		TestSuiteID: testSuiteID,
		Name:        name,
		Status:      status,
		DurationMs:  durationMs,
		Message:     message,
		Trace:       trace,
		FilePath:    filePath,
		Suite:       suite,
		Retries:     int64(retries),
		Flaky:       boolToInt64(flaky),
	})
}

func (d *DB) ListTestSuites(ctx context.Context, snapshotID int64) ([]model.TestSuite, error) {
	rows, err := d.queries().ListTestSuitesBySnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	suites := make([]model.TestSuite, len(rows))
	for i, r := range rows {
		suites[i] = model.TestSuite{
			ID:          r.ID,
			SnapshotID:  r.SnapshotID,
			Name:        r.Name,
			Status:      r.Status,
			PipelineRun: r.PipelineRun,
			ToolName:    r.ToolName,
			ToolVersion: r.ToolVersion,
			Tests:       int(r.Tests),
			Passed:      int(r.Passed),
			Failed:      int(r.Failed),
			Skipped:     int(r.Skipped),
			Pending:     int(r.Pending),
			Other:       int(r.Other),
			Flaky:       int(r.Flaky),
			StartTime:   r.StartTime,
			StopTime:    r.StopTime,
			DurationMs:  r.DurationMs,
			CreatedAt:   parseTime(r.CreatedAt),
		}
	}
	return suites, nil
}

func (d *DB) ListTestCases(ctx context.Context, testSuiteID int64) ([]model.TestCase, error) {
	rows, err := d.queries().ListTestCasesBySuite(ctx, testSuiteID)
	if err != nil {
		return nil, err
	}
	cases := make([]model.TestCase, len(rows))
	for i, r := range rows {
		cases[i] = model.TestCase{
			ID:          r.ID,
			TestSuiteID: r.TestSuiteID,
			Name:        r.Name,
			Status:      r.Status,
			DurationMs:  r.DurationMs,
			Message:     r.Message,
			Trace:       r.Trace,
			FilePath:    r.FilePath,
			Suite:       r.Suite,
			Retries:     int(r.Retries),
			Flaky:       r.Flaky == 1,
		}
	}
	return cases, nil
}

func (d *DB) CreateVulnerabilityReport(ctx context.Context, snapshotID int64, component, arch string, total, critical, high, medium, low, unknown, fixable int) (int64, error) {
	return d.queries().CreateVulnerabilityReport(ctx, dbsqlc.CreateVulnerabilityReportParams{
		SnapshotID: snapshotID,
		Component:  component,
		Arch:       arch,
		Total:      int64(total),
		Critical:   int64(critical),
		High:       int64(high),
		Medium:     int64(medium),
		Low:        int64(low),
		Unknown:    int64(unknown),
		Fixable:    int64(fixable),
	})
}

func (d *DB) CreateVulnerability(ctx context.Context, reportID int64, name, severity, packageName, packageVersion, fixedInVersion, description, link string) error {
	return d.queries().CreateVulnerability(ctx, dbsqlc.CreateVulnerabilityParams{
		ReportID:       reportID,
		Name:           name,
		Severity:       severity,
		PackageName:    packageName,
		PackageVersion: packageVersion,
		FixedInVersion: fixedInVersion,
		Description:    description,
		Link:           link,
	})
}

func (d *DB) ListVulnerabilityReports(ctx context.Context, snapshotID int64) ([]model.VulnerabilityReport, error) {
	rows, err := d.queries().ListVulnerabilityReportsBySnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	reports := make([]model.VulnerabilityReport, len(rows))
	for i, r := range rows {
		reports[i] = model.VulnerabilityReport{
			ID:         r.ID,
			SnapshotID: r.SnapshotID,
			Component:  r.Component,
			Arch:       r.Arch,
			Total:      int(r.Total),
			Critical:   int(r.Critical),
			High:       int(r.High),
			Medium:     int(r.Medium),
			Low:        int(r.Low),
			Unknown:    int(r.Unknown),
			Fixable:    int(r.Fixable),
			CreatedAt:  parseTime(r.CreatedAt),
		}
	}
	return reports, nil
}

func (d *DB) ListVulnerabilities(ctx context.Context, reportID int64) ([]model.Vulnerability, error) {
	rows, err := d.queries().ListVulnerabilitiesByReport(ctx, reportID)
	if err != nil {
		return nil, err
	}
	vulns := make([]model.Vulnerability, len(rows))
	for i, r := range rows {
		vulns[i] = model.Vulnerability{
			ID:             r.ID,
			ReportID:       r.ReportID,
			Name:           r.Name,
			Severity:       r.Severity,
			PackageName:    r.PackageName,
			PackageVersion: r.PackageVersion,
			FixedInVersion: r.FixedInVersion,
			Description:    r.Description,
			Link:           r.Link,
		}
	}
	return vulns, nil
}

func toSnapshotRecord(r dbsqlc.Snapshot) model.SnapshotRecord {
	return model.SnapshotRecord{
		ID:          r.ID,
		Application: r.Application,
		Name:        r.Name,
		TestsPassed: r.TestsPassed == 1,
		CreatedAt:   parseTime(r.CreatedAt),
	}
}
