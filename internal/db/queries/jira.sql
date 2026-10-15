-- name: UpsertJiraIssue :exec
INSERT INTO jira_issues (key, summary, fix_version, issue_type, status, link)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(key, fix_version) DO UPDATE SET
    summary=excluded.summary,
    issue_type=excluded.issue_type,
    status=excluded.status,
    link=excluded.link;

-- name: UpsertReleaseVersion :exec
INSERT INTO release_versions (name, release_date, released, archived, release_ticket_key, release_ticket_assignee, konflux_application, due_date)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    release_date=excluded.release_date,
    released=excluded.released,
    archived=excluded.archived,
    release_ticket_key=excluded.release_ticket_key,
    release_ticket_assignee=excluded.release_ticket_assignee,
    konflux_application=excluded.konflux_application,
    due_date=excluded.due_date;

-- name: GetReleaseVersion :one
SELECT * FROM release_versions WHERE name = ?;

-- name: ListAllReleaseVersions :many
SELECT * FROM release_versions ORDER BY name;

-- name: DeleteAllJiraIssuesForVersion :exec
DELETE FROM jira_issues WHERE fix_version = ?;
