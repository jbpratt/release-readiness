package db

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
)

func TestSelectedStageBuild(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { v := t0.Add(time.Duration(m) * time.Minute); return &v }
	stage := func(name string, created int) {
		t.Helper()
		if err := d.UpsertStagedSnapshot(ctx, name, "3.18.1", "image", "stage"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.CreateSnapshot(ctx, "quay-3-18", name, *at(created)); err != nil {
			t.Fatal(err)
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
	stage("snap-a", 0)
	release("rel-a", "snap-a", "quay-advisory-stage-3-18", "True", "Succeeded", at(10))
	stage("snap-b", 60)
	release("rel-b-old", "snap-b", "quay-advisory-stage-3-18", "True", "Succeeded", at(65))
	release("rel-b", "snap-b", "quay-advisory-stage-3-18", "True", "Succeeded", at(70))
	stage("snap-pending", 120)
	release("rel-pending", "snap-pending", "quay-advisory-stage-3-18", "Unknown", "Progressing", nil)
	stage("snap-failed", 180)
	release("rel-failed", "snap-failed", "quay-advisory-stage-3-18", "False", "Failed", at(190))
	stage("snap-prod", 240)
	release("rel-prod", "snap-prod", "quay-advisory-prod-3-18", "True", "Succeeded", at(250))
	stage("snap-fbc", 300)
	release("rel-fbc", "snap-fbc", "quay-fbc-stage-3-18", "True", "Succeeded", at(310))
	stage("snap-3-17", 360)
	release("rel-3-17", "snap-3-17", "quay-advisory-stage-3-17", "True", "Succeeded", at(370))

	plans := regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`)
	v := &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}
	sel, err := d.SelectedStageBuild(ctx, v, plans)
	if err != nil {
		t.Fatal(err)
	}
	if sel == nil || sel.SnapshotName != "snap-b" || !sel.CompletedAt.Equal(*at(70)) {
		t.Fatalf("selected = %+v; want snap-b completed by rel-b", sel)
	}
	// An empty -stage-release-plan-pattern selects none.
	if sel, err := d.SelectedStageBuild(ctx, v, nil); err != nil || sel != nil {
		t.Errorf("without plans: selected = %+v, %v; want none", sel, err)
	}
}

func TestStagedFBCCatalogs(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	const app = "fbc-quay-3-18"
	t0 := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	stage := func(name string, created int, component, image string) {
		t.Helper()
		if err := d.UpsertStagedSnapshot(ctx, name, "3.18.1", "fbc", "stage"); err != nil {
			t.Fatal(err)
		}
		id, err := d.CreateSnapshot(ctx, app, name, t0.Add(time.Duration(created)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := d.CreateSnapshotComponent(ctx, id, component, image); err != nil {
			t.Fatal(err)
		}
	}
	release := func(name, snapshot, status, reason string, created int) {
		t.Helper()
		if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: name, Application: app, Snapshot: snapshot, ReleasePlan: "quay-fbc-stage-3-18",
			ReleasedStatus: status, ReleasedReason: reason, CreatedAt: t0.Add(time.Duration(created) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	const op, cso = "fbc-quay-3-18-quay-operator", "fbc-quay-3-18-container-security-operator"
	same := "quay.io/redhat-user-workloads/fbc/op@sha256:aaaa"
	// ART stages the same catalog again; its newer Release failed.
	stage("quay-stage-3-18-1-fbc-20261007200000", 0, op, same)
	release("rel-old", "quay-stage-3-18-1-fbc-20261007200000", "True", "Succeeded", 5)
	stage("quay-stage-3-18-1-fbc-20261007210000", 60, op, same)
	release("rel-new", "quay-stage-3-18-1-fbc-20261007210000", "False", "Failed", 65)
	stage("quay-stage-3-18-1-fbc-20261007220000", 120, cso, "quay.io/redhat-user-workloads/fbc/cso@sha256:bbbb")

	got, err := d.StagedFBCCatalogs(ctx, "3.18.1", app)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, c := range got {
		row := c.Snapshot + " " + c.Pullspec
		if c.Release != nil {
			row += " " + c.Release.Name + " " + c.Release.ReleasedReason
		}
		rows = append(rows, row)
	}
	want := []string{
		"quay-stage-3-18-1-fbc-20261007210000 " + same + " rel-new Failed",
		"quay-stage-3-18-1-fbc-20261007220000 quay.io/redhat-user-workloads/fbc/cso@sha256:bbbb",
	}
	if !slices.Equal(rows, want) {
		t.Errorf("catalogs:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func TestProdFBCReleases(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	t0 := time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { v := t0.Add(time.Duration(m) * time.Minute); return &v }
	release := func(name, snapshot, assembly, env, status, reason string, completed *time.Time) {
		t.Helper()
		if err := d.UpsertStagedSnapshot(ctx, snapshot, assembly, "fbc", env); err != nil {
			t.Fatal(err)
		}
		if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
			Name: name, Application: "quay-3-18", Snapshot: snapshot, ReleasePlan: "quay-fbc-" + env,
			ReleasedStatus: status, ReleasedReason: reason, CreatedAt: t0, CompletionTime: completed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := d.ProdFBCReleases(ctx, "3.18.1"); err != nil || got != nil {
		t.Fatalf("before any Release = %+v, %v; want nil", got, err)
	}
	// snap-a shipped twice: counted once.
	release("rel-a", "snap-a", "3.18.1", "prod", "True", "Succeeded", at(10))
	release("rel-a-retry", "snap-a", "3.18.1", "prod", "True", "Succeeded", at(30))
	release("rel-b", "snap-b", "3.18.1", "prod", "True", "Succeeded", at(20))
	release("rel-failed", "snap-failed", "3.18.1", "prod", "False", "Failed", at(40))
	release("rel-stage", "snap-stage", "3.18.1", "stage", "True", "Succeeded", at(50))
	release("rel-other", "snap-other", "3.18.2", "prod", "True", "Succeeded", at(60))
	// An image Snapshot staged for prod is not an FBC.
	if err := d.UpsertStagedSnapshot(ctx, "snap-image", "3.18.1", "image", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertKonfluxRelease(ctx, &model.KonfluxRelease{
		Name: "rel-image", Application: "quay-3-18", Snapshot: "snap-image", ReleasePlan: "quay-advisory-prod-3-18",
		ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: t0, CompletionTime: at(70),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := d.ProdFBCReleases(ctx, "3.18.1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Released != 2 || got.Last == nil || !got.Last.Equal(*at(30)) {
		t.Errorf("3.18.1 = %+v; want 2 released, last %v", got, *at(30))
	}

	// A shipped Release with no completion time leaves last unset.
	release("rel-no-time", "snap-no-time", "3.18.3", "prod", "True", "Succeeded", nil)
	if got, err := d.ProdFBCReleases(ctx, "3.18.3"); err != nil || got == nil || got.Released != 1 || got.Last != nil {
		t.Errorf("3.18.3 = %+v, %v; want 1 released, no last", got, err)
	}
}
