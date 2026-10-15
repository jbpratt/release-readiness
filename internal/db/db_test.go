package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenAddsJiraIssueStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// jira_issues as created before the status column.
	_, err = old.Exec(`
CREATE TABLE jira_issues (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    fix_version TEXT NOT NULL DEFAULT '',
    issue_type  TEXT NOT NULL DEFAULT '',
    link        TEXT NOT NULL DEFAULT ''
);
INSERT INTO jira_issues (key, summary, fix_version, issue_type, link)
VALUES ('PROJQUAY-1', 'fix', 'quay-v3.18.1', 'Bug', 'https://jira.example/browse/PROJQUAY-1');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.ListJiraIssues(t.Context(), "quay-v3.18.1")
		_ = d.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Key != "PROJQUAY-1" || got[0].Status != "" {
			t.Fatalf("ListJiraIssues = %+v, want PROJQUAY-1 with empty status", got)
		}
	}
}
