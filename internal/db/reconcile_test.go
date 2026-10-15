package db

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
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
	qo := reconcile.Source{Name: "quay-operator", SourceRepo: "https://github.com/quay/quay-operator", Commitish: "c1"}
	if err := d.UpsertArtSourceRepo(ctx, "rec-qo", qo.Name, qo.SourceRepo); err != nil {
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
	if got, err := d.SourcesByDigest(ctx, "sha256:aaaa"); err != nil || len(got) != 1 || got[0] != qo {
		t.Errorf("sources of sha256:aaaa: %+v, %v; want only %+v", got, err, qo)
	}
	wantRecords := []reconcile.ARTRecord{
		{Digest: "sha256:aaaa", NVR: "base-1", RecordID: "rec-base"},
		{Digest: "sha256:aaaa", NVR: "qo-1", RecordID: "rec-qo"},
		{Digest: "sha256:bbbb", NVR: "new-1", RecordID: "rec-new"},
	}
	if got, err := d.ARTRecords(ctx, []string{"sha256:bbbb", "sha256:aaaa"}); err != nil || !slices.Equal(got, wantRecords) {
		t.Errorf("ART records: %+v, %v; want %+v", got, err, wantRecords)
	}
	for digest, want := range map[string][2]bool{"sha256:aaaa": {true, false}, "sha256:bbbb": {true, true}, "sha256:cccc": {false, false}} {
		if stored, unchecked, err := d.SourceState(ctx, digest); err != nil || [2]bool{stored, unchecked} != want {
			t.Errorf("source state of %s: %v, %v, %v; want %v", digest, stored, unchecked, err, want)
		}
	}

	first := reconcile.Baseline{Stream: "3.18", Operator: "quay-operator", OCP: "4.21", FBCDigest: "sha256:ffff"}
	if _, ok, err := d.GetBaseline(ctx, "3.18", "quay-operator", "4.21"); err != nil || ok {
		t.Fatalf("baseline before insert: %v, %v", ok, err)
	}
	for _, b := range []reconcile.Baseline{first, {Stream: "3.18", Operator: "quay-operator", OCP: "4.21", FBCDigest: "sha256:eeee"}} {
		if err := d.InsertBaseline(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok, err := d.GetBaseline(ctx, "3.18", "quay-operator", "4.21"); err != nil || !ok || got != first.FBCDigest {
		t.Errorf("baseline: %q, %v, %v; want the first %q", got, ok, err, first.FBCDigest)
	}
}

func TestReconcileStore(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	t0 := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { v := t0.Add(time.Duration(m) * time.Minute); return &v }
	stage := func(snap, assembly, env, digest, status, reason string, completed *time.Time) {
		t.Helper()
		if err := d.UpsertStagedSnapshot(ctx, snap, assembly, "fbc", env); err != nil {
			t.Fatal(err)
		}
		id, err := d.CreateSnapshot(ctx, "fbc-quay", snap, t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.CreateSnapshotComponent(ctx, id, "fbc-quay-operator", "quay.io/fbc@"+digest); err != nil {
			t.Fatal(err)
		}
		if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: "rel-" + snap, Application: "fbc-quay", Snapshot: snap,
			ReleasedStatus: status, ReleasedReason: reason, CreatedAt: t0, CompletionTime: completed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	render := func(digest, state, label string, ch fbc.Channel) {
		t.Helper()
		if err := d.UpsertFBCRender(ctx, fbc.Render{Digest: digest, State: state, LabelNVR: label, Channel: ch, RenderedAt: t0}); err != nil {
			t.Fatal(err)
		}
	}
	stage("a", "3.17.1", "stage", "sha256:a", "True", "Succeeded", at(20))
	stage("b", "3.18.1", "stage", "sha256:b", "True", "Succeeded", at(10))
	stage("unrendered", "3.18.1", "stage", "sha256:c", "True", "Succeeded", at(30))
	stage("prod", "3.18.1", "prod", "sha256:d", "True", "Succeeded", at(40))
	stage("failed", "3.18.1", "stage", "sha256:e", "False", "Failed", at(50))
	stage("new", "3.18.2", "stage", "sha256:f", "True", "Succeeded", at(60))
	render("sha256:a", fbc.StateRendered, "label-a.ocp4.16", fbc.Channel{
		HeadBundlePullspec: "reg/bundle@sha256:bun",
		HeadRelatedImages:  []fbc.RelatedImage{{Name: "quay", Image: "reg/quay@sha256:q"}, {Name: "pg", Image: "reg/pg:15"}},
		Previous:           fbc.Bundle{RelatedImages: []fbc.RelatedImage{{Name: "quay", Image: "reg/quay@sha256:old"}}},
	})
	render("sha256:b", fbc.StateRendered, "label-b.ocp4.16", fbc.Channel{})
	render("sha256:c", fbc.StateFailed, "", fbc.Channel{})
	render("sha256:d", fbc.StateRendered, "label-d", fbc.Channel{})
	render("sha256:e", fbc.StateRendered, "label-e", fbc.Channel{})
	if err := d.UpsertArtHistoryBuild(ctx, artbuild.HistoryBuild{RecordID: "rec-a", Group: "quay-3.17", Type: artbuild.TypeFBC, NVR: "fbc-a.ocp4.16", Digest: "sha256:a", Pullspec: "quay.io/fbc@sha256:a"}); err != nil {
		t.Fatal(err)
	}

	fbcs, err := d.ListStageFBCs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantFBCs := []reconcile.StageFBC{
		{Release: "rel-b", Assembly: "3.18.1", CompletionTime: at(10).Format(time.RFC3339), FBCDigest: "sha256:b", FBCNVR: "label-b.ocp4.16", RenderState: fbc.StateRendered},
		{Release: "rel-a", Assembly: "3.17.1", CompletionTime: at(20).Format(time.RFC3339), FBCDigest: "sha256:a", FBCNVR: "fbc-a.ocp4.16", RenderState: fbc.StateRendered},
		{Release: "rel-unrendered", Assembly: "3.18.1", CompletionTime: at(30).Format(time.RFC3339), FBCDigest: "sha256:c", RenderState: fbc.StateFailed},
		{Release: "rel-new", Assembly: "3.18.2", CompletionTime: at(60).Format(time.RFC3339), FBCDigest: "sha256:f"},
	}
	if !slices.Equal(fbcs, wantFBCs) {
		t.Errorf("stage FBCs:\n%+v\nwant:\n%+v", fbcs, wantFBCs)
	}
	if got, err := d.FBCMemberDigests(ctx, "sha256:a"); err != nil || !slices.Equal(got, []string{"reg/pg:15", "sha256:bun", "sha256:q", "sha256:a"}) {
		t.Errorf("members of sha256:a: %v, %v", got, err)
	}
	if got, err := d.FBCMemberDigests(ctx, "sha256:b"); err != nil || !slices.Equal(got, []string{"sha256:b"}) {
		t.Errorf("members of sha256:b: %v, %v", got, err)
	}

	action := func(digest, release, assembly, status string, m int) reconcile.Action {
		return reconcile.Action{Key: reconcile.ActionKey{FBCDigest: digest, IssueKey: "PROJQUAY-1", Release: release}, Assembly: assembly, FBCNVR: "nvr-" + digest, Status: status, EvidenceJSON: "[]", UpdatedAt: *at(m)}
	}
	first := action("d4", "rel-3", "3.18.2", reconcile.StatusProposed, 2)
	for _, a := range []reconcile.Action{
		action("d1", "rel-1", "3.1.0", reconcile.StatusProposed, 5),
		action("d2", "rel-2", "3.18.1", reconcile.StatusSkipped, 1),
		action("d3", "rel-4", "3.18.2", reconcile.StatusProposed, 3),
		first,
		// A proposed row is final.
		action("d4", "rel-3", "3.18.2", reconcile.StatusSkipped, 4),
	} {
		if err := d.UpsertAction(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok, err := d.FirstIncludedAction(ctx, "3.18", "PROJQUAY-1"); err != nil || !ok || !reflect.DeepEqual(got, first) {
		t.Errorf("first in 3.18: %+v, %v, %v; want %+v", got, ok, err, first)
	}
	if got, ok, err := d.FirstIncludedAction(ctx, "3.1", "PROJQUAY-1"); err != nil || !ok || got.Key.FBCDigest != "d1" {
		t.Errorf("first in 3.1: %+v, %v, %v; want d1", got, ok, err)
	}
	if _, ok, err := d.FirstIncludedAction(ctx, "3.18", "PROJQUAY-2"); err != nil || ok {
		t.Errorf("first PROJQUAY-2: %v, %v; want none", ok, err)
	}
	if got, err := d.PendingActions(ctx, 5); err != nil || len(got) != 3 || !reflect.DeepEqual(got[0], first) || got[1].Key.FBCDigest != "d3" || got[2].Key.FBCDigest != "d1" {
		t.Errorf("pending before writes: %+v, %v; want first, d3 and d1", got, err)
	}

	// The writer moves proposed to error to applied; the reconciler
	// cannot overwrite either, nor the writer go back an attempt.
	failed := first
	failed.Status, failed.Error, failed.VersionStep, failed.Attempts = reconcile.StatusError, "boom", "edited", 1
	applied := failed
	applied.Status, applied.Error, applied.Attempts = reconcile.StatusApplied, "", 2
	for _, a := range []reconcile.Action{failed, applied, first, failed} {
		if err := d.UpsertAction(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok, err := d.FirstIncludedAction(ctx, "3.18", "PROJQUAY-1"); err != nil || !ok || !reflect.DeepEqual(got, applied) {
		t.Errorf("after writes: %+v, %v, %v; want %+v", got, ok, err, applied)
	}
	if got, err := d.PendingActions(ctx, 5); err != nil || len(got) != 2 || got[0].Key.FBCDigest != "d3" || got[1].Key.FBCDigest != "d1" {
		t.Errorf("pending: %+v, %v; want d3 and d1", got, err)
	}

	if done, err := d.RunDone(ctx, "rel-b", "sha256:b"); err != nil || done {
		t.Errorf("unmarked run done: %v, %v", done, err)
	}
	for range 2 {
		if err := d.MarkRun(ctx, "rel-b", "sha256:b"); err != nil {
			t.Fatal(err)
		}
	}
	if done, err := d.RunDone(ctx, "rel-b", "sha256:b"); err != nil || !done {
		t.Errorf("run done: %v, %v", done, err)
	}
	if done, err := d.RunDone(ctx, "rel-a", "sha256:b"); err != nil || done {
		t.Errorf("other release done: %v, %v", done, err)
	}
}
