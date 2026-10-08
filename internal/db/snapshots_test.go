package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotsOrderedByCreatedAt(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	// Inserted newest first, so id order is the reverse of creation order.
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"newest", "middle", "oldest"} {
		if _, err := d.CreateSnapshot(ctx, "quay-3-18", name, base.Add(-time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"newest", "middle", "oldest"}
	for _, app := range []string{"", "quay-3-18"} {
		snaps, err := d.ListSnapshots(ctx, app, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(snaps) != len(want) {
			t.Fatalf("ListSnapshots(%q) returned %d snapshots, want %d", app, len(snaps), len(want))
		}
		for i, s := range snaps {
			if s.Name != want[i] {
				t.Errorf("ListSnapshots(%q)[%d] = %q, want %q", app, i, s.Name, want[i])
			}
		}
	}
}
