package db

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/github"
)

func TestEvidenceScans(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	ev := github.Evidence{Key: "PROJQUAY-1", CommitSHA: "c1", CommitURL: "https://github.com/quay/quay/commit/c1", PRURL: "https://github.com/quay/quay/pull/1", Source: github.SourceCompare}
	scan := github.Scan{Snapshot: "snap", Component: "quay", ImageDigest: "sha256:1", Repo: "https://github.com/quay/quay", BaseSHA: "base", HeadSHA: "head",
		State: github.StateUnknown, Reason: github.ReasonAPIError, CheckedAt: t0}
	if err := d.StoreEvidenceScan(ctx, scan); err != nil {
		t.Fatal(err)
	}
	if got, err := d.CompleteScan(ctx, scan.Repo, "base", "head"); got != nil || err != nil {
		t.Errorf("CompleteScan of unknown scan = %+v, %v; want nil", got, err)
	}

	scan.State, scan.Reason, scan.Evidence = github.StateComplete, "", []github.Evidence{ev, {Key: "PROJQUAY-2", CommitSHA: "c2", Source: github.SourceCompare}}
	if err := d.StoreEvidenceScan(ctx, scan); err != nil {
		t.Fatal(err)
	}
	scan.Evidence = []github.Evidence{ev}
	if err := d.StoreEvidenceScan(ctx, scan); err != nil {
		t.Fatal(err)
	}
	other := github.Scan{Snapshot: "snap", Component: "operator", ImageDigest: "sha256:2", State: github.StateUnknown, Reason: github.ReasonNoProvenance, CheckedAt: t0}
	if err := d.StoreEvidenceScan(ctx, other); err != nil {
		t.Fatal(err)
	}

	scans, err := d.ListEvidenceScans(ctx, "snap")
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 2 || scans[0].Component != "operator" || scans[0].Evidence != nil ||
		scans[1].State != github.StateComplete || !scans[1].CheckedAt.Equal(t0) || !slices.Equal(scans[1].Evidence, []github.Evidence{ev}) {
		t.Errorf("scans = %+v, want operator unknown and quay complete with only %+v", scans, ev)
	}
	got, err := d.CompleteScan(ctx, scan.Repo, "base", "head")
	if err != nil || got == nil || got.Snapshot != "snap" || !slices.Equal(got.Evidence, []github.Evidence{ev}) {
		t.Errorf("CompleteScan = %+v, %v; want snap's quay scan", got, err)
	}
}
