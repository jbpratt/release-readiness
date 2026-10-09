package db

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
)

func TestLatestStagedSnapshot(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		name, assembly, kind, env string
		created                   time.Time
	}{
		{"image-old", "3.18.1", "image", "stage", t0},
		{"image-new", "3.18.1", "image", "stage", t0.Add(time.Hour)},
		{"fbc-new", "3.18.1", "fbc", "stage", t0.Add(2 * time.Hour)},
		{"fbc-old", "3.18.1", "fbc", "stage", t0},
		{"image-prod", "3.18.1", "image", "prod", t0.Add(3 * time.Hour)},
		{"image-other", "3.18.2", "image", "stage", t0.Add(3 * time.Hour)},
	} {
		if err := d.UpsertStagedSnapshot(ctx, s.name, s.assembly, s.kind, s.env, s.created); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"image-new-r1", "image-new-r2"} {
		if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: name, Application: "quay-3-18", Snapshot: "image-new",
			ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: t0.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	img, err := d.LatestStagedSnapshot(ctx, "3.18.1", "image")
	if err != nil {
		t.Fatal(err)
	}
	if img == nil || img.Name != "image-new" || !img.CreatedAt.Equal(t0.Add(time.Hour)) || img.Release == nil || img.Release.Name != "image-new-r2" {
		t.Errorf("image = %+v, want image-new with Release image-new-r2", img)
	}
	fbc, err := d.LatestStagedSnapshot(ctx, "3.18.1", "fbc")
	if err != nil {
		t.Fatal(err)
	}
	if fbc == nil || fbc.Name != "fbc-new" || fbc.Release != nil {
		t.Errorf("fbc = %+v, want fbc-new without a Release", fbc)
	}
	if none, err := d.LatestStagedSnapshot(ctx, "3.17.6", "image"); none != nil || err != nil {
		t.Errorf("3.17.6 = %+v, %v; want nil", none, err)
	}
}

func TestSelectedStageBuilds(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { v := t0.Add(time.Duration(m) * time.Minute); return &v }
	digest := func(c byte) string { return "sha256:" + strings.Repeat(string(c), 64) }
	stage := func(name string, created int, images ...string) {
		t.Helper()
		if err := d.UpsertStagedSnapshot(ctx, name, "3.18.1", "image", "stage", *at(created)); err != nil {
			t.Fatal(err)
		}
		id, err := d.CreateSnapshot(ctx, "quay-3-18", name, *at(created))
		if err != nil {
			t.Fatal(err)
		}
		for i, img := range images {
			if err := d.CreateSnapshotComponent(ctx, id, fmt.Sprintf("c%d", i), img); err != nil {
				t.Fatal(err)
			}
		}
	}
	release := func(name, snapshot, plan, status, reason string, completed *time.Time) {
		t.Helper()
		if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: name, Application: "quay-3-18", Snapshot: snapshot, ReleasePlan: plan,
			ReleasedStatus: status, ReleasedReason: reason, CreatedAt: t0, CompletionTime: completed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	stage("snap-a", 0, "quay.io/x/a@"+digest('a'))
	release("rel-a", "snap-a", "stage-plan", "True", "Succeeded", at(10))
	stage("snap-b", 60,
		"quay.io/x/full@"+digest('1'),
		"quay.io/x/short@"+digest('2'),
		"quay.io/x/unresolved@"+digest('3'),
		"quay.io/x/tag:v1")
	release("rel-b-old", "snap-b", "stage-plan", "True", "Succeeded", at(65))
	release("rel-b", "snap-b", "stage-plan", "True", "Succeeded", at(70))
	stage("snap-pending", 120)
	release("rel-pending", "snap-pending", "stage-plan", "Unknown", "Progressing", nil)
	stage("snap-failed", 180)
	release("rel-failed", "snap-failed", "stage-plan", "False", "Failed", at(190))
	stage("snap-prod", 240)
	release("rel-prod", "snap-prod", "prod-plan", "True", "Succeeded", at(250))
	for _, b := range []artbuild.Build{
		{Digest: digest('1'), State: artbuild.StateResolved, UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: strings.Repeat("f", 40)},
		{Digest: digest('2'), State: artbuild.StateResolved, UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: "abc123"},
		{Digest: digest('3'), State: artbuild.StateUnresolved},
	} {
		b.CheckedAt = t0
		if err := d.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	v := &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}
	got, err := d.SelectedStageBuilds(ctx, v, []string{"stage-plan"})
	if err != nil {
		t.Fatal(err)
	}
	sel := got.Selected
	if sel == nil || sel.SnapshotName != "snap-b" || sel.ReleaseName != "rel-b" || !sel.SelectedAt.Equal(*at(70)) ||
		sel.RRURL != "/releases/quay-v3.18.1/snapshots?snapshot=snap-b" {
		t.Fatalf("selected = %+v, want snap-b by rel-b", sel)
	}
	if got.Previous == nil || got.Previous.SnapshotName != "snap-a" {
		t.Errorf("previous = %+v, want snap-a", got.Previous)
	}
	want := []model.SelectedBuildComponent{
		{Name: "c0", ImageDigest: digest('1'), UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: strings.Repeat("f", 40), ProvenanceState: "resolved"},
		{Name: "c1", ImageDigest: digest('2'), UpstreamRepo: "https://github.com/quay/quay", UpstreamSHA: "abc123", ProvenanceState: "unknown"},
		{Name: "c2", ImageDigest: digest('3'), ProvenanceState: "unknown"},
		{Name: "c3", ProvenanceState: "unknown"},
	}
	if !slices.Equal(sel.Components, want) {
		t.Errorf("components = %+v, want %+v", sel.Components, want)
	}

	for _, tc := range []struct {
		name    string
		version string
		plans   []string
		reason  string
	}{
		{"empty plan config", "quay-v3.18.1", nil, "stage release plan not configured"},
		{"plan substring", "quay-v3.18.1", []string{"stage"}, "no successful stage release of a staged image snapshot"},
		{"non-concrete version", "quay-v3.18.z", []string{"stage-plan"}, "not a concrete quay-vX.Y.Z version"},
	} {
		got, err := d.SelectedStageBuilds(ctx, &model.ReleaseVersion{Name: tc.version, KonfluxApplication: "quay-3-18"}, tc.plans)
		if err != nil || got.Selected != nil || got.Reason != tc.reason {
			t.Errorf("%s: got %+v, %v; want reason %q", tc.name, got, err, tc.reason)
		}
	}
}
