package db

import (
	"path/filepath"
	"testing"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/reconcile"
)

func TestProvenanceStore(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	for _, b := range []artbuild.HistoryBuild{
		{RecordID: "rec-qo", Group: "quay-3.18", Type: artbuild.TypeImage, NVR: "qo-1", Digest: "sha256:aaaa", Pullspec: "x@sha256:aaaa", Commitish: "c1", StartTime: "2026-10-01T00:00:00"},
		{RecordID: "rec-base", Group: "quay-3.18", Type: artbuild.TypeImage, NVR: "base-1", Digest: "sha256:aaaa", Pullspec: "x@sha256:aaaa", Commitish: "c2", StartTime: "2026-10-02T00:00:00"},
		{RecordID: "rec-new", Group: "quay-3.18", Type: artbuild.TypeImage, NVR: "new-1", Digest: "sha256:bbbb", Pullspec: "x@sha256:bbbb", StartTime: "2026-10-03T00:00:00"},
	} {
		if err := d.UpsertArtHistoryBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	qo := reconcile.Source{RecordID: "rec-qo", Name: "quay-operator", NVR: "qo-1", Digest: "sha256:aaaa", SourceRepo: "https://github.com/quay/quay-operator", Commitish: "c1"}
	if err := d.UpsertSource(ctx, qo); err != nil {
		t.Fatal(err)
	}
	// rec-base was checked and names no source repo.
	if err := d.UpsertArtSourceRepo(ctx, "rec-base", "base-rhel9", ""); err != nil {
		t.Fatal(err)
	}

	pending, err := d.ArtHistoryWithoutSource(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RecordID != "rec-new" || pending[0].Pullspec != "x@sha256:bbbb" {
		t.Errorf("without source: %+v, want only rec-new", pending)
	}
	if got, ok, err := d.SourceByRecord(ctx, "rec-qo"); err != nil || !ok || got != qo {
		t.Errorf("source of rec-qo: %+v, %v, %v; want %+v", got, ok, err, qo)
	}
	for _, id := range []string{"rec-base", "rec-new"} {
		if got, ok, err := d.SourceByRecord(ctx, id); err != nil || ok {
			t.Errorf("source of %s: %+v, %v, %v; want none", id, got, ok, err)
		}
	}
	if got, err := d.SourcesByDigest(ctx, "sha256:aaaa"); err != nil || len(got) != 1 || got[0] != qo {
		t.Errorf("sources of sha256:aaaa: %+v, %v; want only %+v", got, err, qo)
	}

	first := reconcile.Baseline{Stream: "3.18", Operator: "quay-operator", OCP: "4.21", FBCDigest: "sha256:ffff", FBCNVR: "fbc-1"}
	if _, ok, err := d.GetBaseline(ctx, "3.18", "quay-operator", "4.21"); err != nil || ok {
		t.Fatalf("baseline before insert: %v, %v", ok, err)
	}
	for _, b := range []reconcile.Baseline{first, {Stream: "3.18", Operator: "quay-operator", OCP: "4.21", FBCDigest: "sha256:eeee", FBCNVR: "fbc-2"}} {
		if err := d.InsertBaseline(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok, err := d.GetBaseline(ctx, "3.18", "quay-operator", "4.21"); err != nil || !ok || got != first {
		t.Errorf("baseline: %+v, %v, %v; want the first %+v", got, ok, err, first)
	}
}
