package db

import (
	"context"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertJiraIssue(ctx context.Context, issue *model.JiraIssueRecord) error {
	return d.queries().UpsertJiraIssue(ctx, dbsqlc.UpsertJiraIssueParams{
		Key:        issue.Key,
		Summary:    issue.Summary,
		Status:     issue.Status,
		Priority:   issue.Priority,
		Labels:     issue.Labels,
		FixVersion: issue.FixVersion,
		Assignee:   issue.Assignee,
		IssueType:  issue.IssueType,
		Link:       issue.Link,
		QaContact:  issue.QAContact,
	})
}

// ListJiraIssues returns issues for a fixVersion.
func (d *DB) ListJiraIssues(ctx context.Context, fixVersion string) ([]model.JiraIssueRecord, error) {
	query := `SELECT key, summary, status, priority, fix_version, assignee, issue_type, link, qa_contact
		FROM jira_issues WHERE fix_version = ? ORDER BY key`

	rows, err := d.dbtx.QueryContext(ctx, query, fixVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var issues []model.JiraIssueRecord
	for rows.Next() {
		var i model.JiraIssueRecord
		if err := rows.Scan(&i.Key, &i.Summary, &i.Status, &i.Priority,
			&i.FixVersion, &i.Assignee, &i.IssueType, &i.Link, &i.QAContact); err != nil {
			return nil, err
		}
		issues = append(issues, i)
	}
	return issues, rows.Err()
}

func (d *DB) GetIssueSummary(ctx context.Context, fixVersion string) (*model.IssueSummary, error) {
	row, err := d.queries().GetIssueSummary(ctx, fixVersion)
	if err != nil {
		return nil, err
	}
	return &model.IssueSummary{
		Total:    int(row.Total),
		Verified: int(row.Verified),
		Open:     int(row.Open),
		CVEs:     int(row.Cves),
	}, nil
}

// GetIssueSummariesBatch returns aggregate counts for multiple fixVersions in a single query.
// Stays hand-written due to variable IN clause.
func (d *DB) GetIssueSummariesBatch(ctx context.Context, fixVersions []string) (map[string]*model.IssueSummary, error) {
	if len(fixVersions) == 0 {
		return map[string]*model.IssueSummary{}, nil
	}

	placeholders := make([]string, len(fixVersions))
	args := make([]interface{}, len(fixVersions))
	for i, v := range fixVersions {
		placeholders[i] = "?"
		args[i] = v
	}

	query := `
		SELECT fix_version,
			COUNT(*) AS total,
			SUM(CASE WHEN LOWER(status) IN ('closed', 'verified', 'done', 'release pending') THEN 1 ELSE 0 END) AS verified,
			SUM(CASE WHEN LOWER(status) NOT IN ('closed', 'verified', 'done', 'release pending') THEN 1 ELSE 0 END) AS open,
			SUM(CASE WHEN LOWER(issue_type) = 'vulnerability' OR LOWER(labels) LIKE '%cve%' THEN 1 ELSE 0 END) AS cves
		FROM jira_issues
		WHERE fix_version IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY fix_version`

	rows, err := d.dbtx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]*model.IssueSummary, len(fixVersions))
	for rows.Next() {
		var fixVersion string
		var s model.IssueSummary
		if err := rows.Scan(&fixVersion, &s.Total, &s.Verified, &s.Open, &s.CVEs); err != nil {
			return nil, err
		}
		result[fixVersion] = &s
	}
	return result, rows.Err()
}

func (d *DB) UpsertReleaseVersion(ctx context.Context, v *model.ReleaseVersion) error {
	relDate := ""
	if v.ReleaseDate != nil {
		relDate = v.ReleaseDate.UTC().Format(time.RFC3339)
	}
	dueDate := ""
	if v.DueDate != nil {
		dueDate = v.DueDate.UTC().Format(time.RFC3339)
	}
	return d.queries().UpsertReleaseVersion(ctx, dbsqlc.UpsertReleaseVersionParams{
		Name:                  v.Name,
		ReleaseDate:           relDate,
		Released:              boolToInt64(v.Released),
		Archived:              boolToInt64(v.Archived),
		ReleaseTicketKey:      v.ReleaseTicketKey,
		ReleaseTicketAssignee: v.ReleaseTicketAssignee,
		KonfluxApplication:    v.KonfluxApplication,
		DueDate:               dueDate,
	})
}

func (d *DB) GetReleaseVersion(ctx context.Context, name string) (*model.ReleaseVersion, error) {
	row, err := d.queries().GetReleaseVersion(ctx, name)
	if err != nil {
		return nil, err
	}
	return toReleaseVersion(row.Name, row.ReleaseDate, row.Released, row.Archived,
		row.ReleaseTicketKey, row.ReleaseTicketAssignee, row.KonfluxApplication, row.DueDate), nil
}

func (d *DB) ListAllReleaseVersions(ctx context.Context) ([]model.ReleaseVersion, error) {
	rows, err := d.queries().ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	versions := make([]model.ReleaseVersion, len(rows))
	for i, r := range rows {
		versions[i] = *toReleaseVersion(r.Name, r.ReleaseDate, r.Released, r.Archived,
			r.ReleaseTicketKey, r.ReleaseTicketAssignee, r.KonfluxApplication, r.DueDate)
	}
	return versions, nil
}

// DeleteJiraIssuesNotIn removes issues for a fixVersion that are not in the given keys slice.
// Stays hand-written due to variable NOT IN clause.
func (d *DB) DeleteJiraIssuesNotIn(ctx context.Context, fixVersion string, keys []string) error {
	if len(keys) == 0 {
		return d.queries().DeleteAllJiraIssuesForVersion(ctx, fixVersion)
	}
	placeholders := make([]string, len(keys))
	args := make([]interface{}, 0, len(keys)+1)
	args = append(args, fixVersion)
	for i, k := range keys {
		placeholders[i] = "?"
		args = append(args, k)
	}
	query := `DELETE FROM jira_issues WHERE fix_version = ? AND key NOT IN (` + strings.Join(placeholders, ",") + `)`
	_, err := d.dbtx.ExecContext(ctx, query, args...)
	return err
}

func toReleaseVersion(name, relDate string, released, archived int64, ticketKey, ticketAssignee, konfluxApp, dueDate string) *model.ReleaseVersion {
	return &model.ReleaseVersion{
		Name:                  name,
		ReleaseDate:           parseOptionalTime(relDate),
		Released:              released == 1,
		Archived:              archived == 1,
		ReleaseTicketKey:      ticketKey,
		ReleaseTicketAssignee: ticketAssignee,
		KonfluxApplication:    konfluxApp,
		DueDate:               parseOptionalTime(dueDate),
	}
}
