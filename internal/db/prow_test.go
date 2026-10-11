package db

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/prow"
)

func TestUpsertProwRunPendingToTerminal(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	started := time.Date(2026, 10, 8, 16, 31, 9, 0, time.UTC)
	run := &prow.Run{
		JobName: "job", BuildID: "1", Application: "quay-3-18",
		State: "pending", StartedAt: &started, ArtifactState: prow.ArtifactMissing,
		FetchedAt: started,
	}
	if err := d.UpsertProwRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if ids, err := d.ListFinishedProwBuildIDs(ctx, "job"); err != nil || len(ids) != 0 {
		t.Fatalf("finished after pending upsert = %v, %v, want none", ids, err)
	}

	completed := started.Add(2 * time.Hour)
	run.State, run.CompletedAt, run.ArtifactState = "success", &completed, prow.ArtifactPresent
	run.CatalogRef = "quay.io/x/art-fbc@sha256:aaa"
	run.Images = []prow.Image{{Role: "catalog", Digest: "sha256:aaa"}, {Role: "quay", Digest: "sha256:bbb"}}
	// Upserting twice must not duplicate images.
	for range 2 {
		if err := d.UpsertProwRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := d.ListFinishedProwBuildIDs(ctx, "job"); err != nil || len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("finished after terminal upsert = %v, %v, want [1]", ids, err)
	}

	runs, err := d.ListProwRunsByApplication(ctx, "quay-3-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.State != "success" || got.CompletedAt == nil || !got.CompletedAt.Equal(completed) ||
		got.ArtifactState != prow.ArtifactPresent || len(got.Images) != 2 || got.Images[1] != run.Images[1] {
		t.Errorf("run = %+v", got)
	}
}

// A finished run an older binary stored as "missing" is read once more.
func TestListFinishedProwBuildIDs(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	at := time.Date(2026, 10, 8, 23, 49, 38, 0, time.UTC)
	for id, state := range map[string]string{"1": prow.ArtifactMissing, "2": prow.ArtifactOLMCSVs, "3": prow.ArtifactNone, "4": prow.ArtifactPresent} {
		run := &prow.Run{JobName: "job", BuildID: id, Application: "quay-3-18", State: "failure", StartedAt: &at, CompletedAt: &at, ArtifactState: state, FetchedAt: at}
		if err := d.UpsertProwRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := d.ListFinishedProwBuildIDs(ctx, "job")
	slices.Sort(ids)
	if err != nil || !slices.Equal(ids, []string{"2", "3", "4"}) {
		t.Errorf("finished = %v, %v, want [2 3 4]", ids, err)
	}
}
