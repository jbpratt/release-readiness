-- name: UpsertJiraIssue :exec
INSERT INTO jira_issues (key, summary, status, priority, labels, fix_version, assignee, issue_type, resolution, link, qa_contact, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key, fix_version) DO UPDATE SET
    summary=excluded.summary,
    status=excluded.status,
    priority=excluded.priority,
    labels=excluded.labels,
    assignee=excluded.assignee,
    issue_type=excluded.issue_type,
    resolution=excluded.resolution,
    link=excluded.link,
    qa_contact=excluded.qa_contact,
    updated_at=excluded.updated_at;

-- name: GetIssueSummary :one
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(CASE WHEN LOWER(status) IN ('closed', 'verified', 'done') THEN 1 ELSE 0 END), 0) AS INTEGER) AS verified,
    CAST(COALESCE(SUM(CASE WHEN LOWER(status) NOT IN ('closed', 'verified', 'done') THEN 1 ELSE 0 END), 0) AS INTEGER) AS open,
    CAST(COALESCE(SUM(CASE WHEN LOWER(issue_type) = 'vulnerability' OR LOWER(labels) LIKE '%cve%' THEN 1 ELSE 0 END), 0) AS INTEGER) AS cves,
    CAST(COALESCE(SUM(CASE WHEN LOWER(issue_type) = 'bug' THEN 1 ELSE 0 END), 0) AS INTEGER) AS bugs
FROM jira_issues
WHERE fix_version = ?;

-- name: UpsertReleaseVersion :exec
INSERT INTO release_versions (name, description, release_date, released, archived, release_ticket_key, release_ticket_assignee, konflux_application, due_date)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    description=excluded.description,
    release_date=excluded.release_date,
    released=excluded.released,
    archived=excluded.archived,
    release_ticket_key=excluded.release_ticket_key,
    release_ticket_assignee=excluded.release_ticket_assignee,
    konflux_application=excluded.konflux_application,
    due_date=excluded.due_date;

-- name: GetReleaseVersion :one
SELECT name, description, release_date, released, archived, release_ticket_key, release_ticket_assignee, konflux_application, due_date
FROM release_versions WHERE name = ?;

-- name: ListAllReleaseVersions :many
SELECT name, description, release_date, released, archived, release_ticket_key, release_ticket_assignee, konflux_application, due_date
FROM release_versions
ORDER BY name;

-- name: DeleteAllJiraIssuesForVersion :exec
DELETE FROM jira_issues WHERE fix_version = ?;
