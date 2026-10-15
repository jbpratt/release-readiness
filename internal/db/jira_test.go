package db

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/quay/release-readiness/internal/model"
)

func TestJiraIssueStatusRoundTrip(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	issue := model.JiraIssueRecord{
		Key: "PROJQUAY-1", Summary: "fix", FixVersion: "quay-v3.18.1",
		IssueType: "Bug", Status: "Verified", Link: "https://jira.example/browse/PROJQUAY-1",
	}
	if err := d.UpsertJiraIssue(ctx, &issue); err != nil {
		t.Fatal(err)
	}
	// A later sync sees the issue moved on: the upsert must update the status.
	issue.Status = "Release Pending"
	if err := d.UpsertJiraIssue(ctx, &issue); err != nil {
		t.Fatal(err)
	}

	got, err := d.ListJiraIssues(ctx, "quay-v3.18.1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []model.JiraIssueRecord{issue}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListJiraIssues = %+v, want %+v", got, want)
	}
}
