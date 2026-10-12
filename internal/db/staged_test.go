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
