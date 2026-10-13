package db

import (
	"context"
	"strings"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertJiraIssue(ctx context.Context, issue *model.JiraIssueRecord) error {
	return d.queries().UpsertJiraIssue(ctx, dbsqlc.UpsertJiraIssueParams{
		Key:        issue.Key,
		Summary:    issue.Summary,
		FixVersion: issue.FixVersion,
		IssueType:  issue.IssueType,
		Link:       issue.Link,
	})
}

// ListJiraIssues returns issues for a fixVersion.
func (d *DB) ListJiraIssues(ctx context.Context, fixVersion string) ([]model.JiraIssueRecord, error) {
	query := `SELECT key, summary, fix_version, issue_type, link
		FROM jira_issues WHERE fix_version = ? ORDER BY key`

	rows, err := d.dbtx.QueryContext(ctx, query, fixVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var issues []model.JiraIssueRecord
	for rows.Next() {
		var i model.JiraIssueRecord
		if err := rows.Scan(&i.Key, &i.Summary, &i.FixVersion, &i.IssueType, &i.Link); err != nil {
			return nil, err
		}
		issues = append(issues, i)
	}
	return issues, rows.Err()
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
			SUM(CASE WHEN LOWER(issue_type) = 'vulnerability' THEN 1 ELSE 0 END) AS cves
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
		if err := rows.Scan(&fixVersion, &s.CVEs); err != nil {
			return nil, err
		}
		result[fixVersion] = &s
	}
	return result, rows.Err()
}

func (d *DB) UpsertReleaseVersion(ctx context.Context, v *model.ReleaseVersion) error {
	return d.queries().UpsertReleaseVersion(ctx, dbsqlc.UpsertReleaseVersionParams{
		Name:                  v.Name,
		ReleaseDate:           formatOptionalTime(v.ReleaseDate),
		Released:              boolToInt64(v.Released),
		Archived:              boolToInt64(v.Archived),
		ReleaseTicketKey:      v.ReleaseTicketKey,
		ReleaseTicketAssignee: v.ReleaseTicketAssignee,
		KonfluxApplication:    v.KonfluxApplication,
		DueDate:               formatOptionalTime(v.DueDate),
	})
}

func (d *DB) GetReleaseVersion(ctx context.Context, name string) (*model.ReleaseVersion, error) {
	row, err := d.queries().GetReleaseVersion(ctx, name)
	if err != nil {
		return nil, err
	}
	return toReleaseVersion(row), nil
}

func (d *DB) ListAllReleaseVersions(ctx context.Context) ([]model.ReleaseVersion, error) {
	rows, err := d.queries().ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	versions := make([]model.ReleaseVersion, len(rows))
	for i, r := range rows {
		versions[i] = *toReleaseVersion(r)
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

func toReleaseVersion(r dbsqlc.ReleaseVersion) *model.ReleaseVersion {
	return &model.ReleaseVersion{
		Name:                  r.Name,
		ReleaseDate:           parseOptionalTime(r.ReleaseDate),
		Released:              r.Released == 1,
		Archived:              r.Archived == 1,
		ReleaseTicketKey:      r.ReleaseTicketKey,
		ReleaseTicketAssignee: r.ReleaseTicketAssignee,
		KonfluxApplication:    r.KonfluxApplication,
		DueDate:               parseOptionalTime(r.DueDate),
	}
}
