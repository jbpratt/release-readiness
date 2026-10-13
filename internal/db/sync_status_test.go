package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSyncStatus(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, u := range []struct {
		source string
		at     time.Time
	}{{"jira", t0}, {"konflux", t0}, {"jira", t0.Add(time.Minute)}} {
		if err := d.UpsertSyncStatus(ctx, u.source, u.at); err != nil {
			t.Fatal(err)
		}
	}

	got, err := d.SyncStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["jira"].Equal(t0.Add(time.Minute)) || !got["konflux"].Equal(t0) {
		t.Errorf("sync status = %v, want jira at %v and konflux at %v", got, t0.Add(time.Minute), t0)
	}
}
