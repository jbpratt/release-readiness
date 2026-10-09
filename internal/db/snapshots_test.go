package db

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListReleaseSnapshots(t *testing.T) {
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
	if err := d.UpsertStagedSnapshot(ctx, "middle", "3.18.1", "image", "stage", base); err != nil {
		t.Fatal(err)
	}

	snaps, err := d.ListReleaseSnapshots(ctx, "quay-3-18", []string{"quay-3-18"}, false, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range snaps {
		got = append(got, s.Name+":"+s.ArtKind)
	}
	if want := []string{"newest:", "middle:image", "oldest:"}; !slices.Equal(got, want) {
		t.Errorf("snapshots = %v, want %v", got, want)
	}
}
