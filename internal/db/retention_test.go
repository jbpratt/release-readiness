package db

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/fbc"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/scan"
)

// Prune deletes old rows no kept row needs, with their children, and keeps
// every kept and every young row; a second Prune finds nothing to delete.
func TestPrune(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	cutoff := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	old, young := cutoff.Add(-48*time.Hour), cutoff.Add(time.Hour)
	image := func(digest string) string { return "quay.io/x/y@sha256:" + digest }
	snapshot := func(app, name string, at time.Time, digest string) {
		t.Helper()
		id, err := d.CreateSnapshot(ctx, app, name, at)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.CreateSnapshotComponent(ctx, id, "c", image(digest)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot("quay-3-18", "staged", old, "staged")
	snapshot("quay-3-18", "shipped", old, "shipped")
	snapshot("quay-3-18", "junk", old, "junk")
	snapshot("quay-3-18", "young", young, "young")
	snapshot("quay-3-17", "newest", old, "newest")
	// gone is staged with no stored Snapshot.
	for _, name := range []string{"staged", "gone"} {
		if err := d.UpsertStagedSnapshot(ctx, name, "3.18.1", "fbc", "stage"); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []model.KonfluxRelease{
		{Name: "succeeded", Snapshot: "shipped", ReleasedReason: "Succeeded", CreatedAt: old},
		{Name: "progressing", Snapshot: "junk", ReleasedReason: "Progressing", CreatedAt: old},
		{Name: "failed", Snapshot: "junk", ReleasedReason: "Failed", CreatedAt: old},
		{Name: "failed-young", Snapshot: "junk", ReleasedReason: "Failed", CreatedAt: young},
		{Name: "failed-staged", Snapshot: "staged", ReleasedReason: "Failed", CreatedAt: old},
	} {
		if err := d.UpsertKonfluxRelease(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	at := func(t time.Time) *time.Time { return &t }
	for _, r := range []prow.Run{
		{JobName: "j", BuildID: "1", StartedAt: at(old), Images: []prow.Image{{Role: "quay", Digest: "sha256:run1"}}, InstallFailed: true},
		{JobName: "j", BuildID: "2", StartedAt: at(old), CatalogRef: image("staged")},
		{JobName: "j", BuildID: "3", StartedAt: at(old.Add(time.Hour)), CatalogRef: image("catalog"), Images: []prow.Image{{Role: "quay", Digest: "sha256:run3"}}},
		{JobName: "k", BuildID: "4", StartedAt: at(old)},
		{JobName: "k", BuildID: "5", StartedAt: at(young)},
	} {
		if err := d.UpsertProwRun(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.UpsertArtHistoryBuild(ctx, artbuild.HistoryBuild{RecordID: "r", Group: "g", Type: "image", NVR: "n", Digest: "sha256:history"}); err != nil {
		t.Fatal(err)
	}
	// Each cache holds an old row of every reference kind, an old junk one
	// and a young unreferenced one.
	cached := map[string]time.Time{"staged": old, "history": old, "catalog": old, "run3": old, "junk": old, "run1": old, "fresh": young}
	for digest, at := range cached {
		digest = "sha256:" + digest
		if err := d.UpsertArtBuild(ctx, artbuild.Build{Digest: digest, State: artbuild.StateUnresolved, CheckedAt: at}); err != nil {
			t.Fatal(err)
		}
		if err := d.UpsertImageScan(ctx, scan.Scan{Digest: digest, State: "pending", CheckedAt: at}); err != nil {
			t.Fatal(err)
		}
		related := []fbc.RelatedImage{{Name: "quay", Image: image("related")}}
		if err := d.UpsertFBCRender(ctx, fbc.Render{Digest: digest, State: "rendered", Channel: fbc.Channel{HeadRelatedImages: related}, RenderedAt: at}); err != nil {
			t.Fatal(err)
		}
	}

	rows := func(query string) string {
		t.Helper()
		var s string
		if err := d.conn.QueryRowContext(ctx, "SELECT COALESCE(group_concat(k, ' '), '') FROM ("+query+" ORDER BY 1)").Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	digests := func(table string) string {
		return strings.ReplaceAll(rows("SELECT digest AS k FROM "+table), "sha256:", "")
	}
	state := func() map[string]string {
		return map[string]string{
			"snapshots":             rows("SELECT name AS k FROM snapshots"),
			"snapshot_components":   rows("SELECT substr(image_url, instr(image_url, ':') + 1) AS k FROM snapshot_components"),
			"staged_snapshots":      rows("SELECT name AS k FROM staged_snapshots"),
			"konflux_releases":      rows("SELECT name AS k FROM konflux_releases"),
			"konflux_release_art":   rows("SELECT name AS k FROM konflux_release_art"),
			"prow_runs":             rows("SELECT build_id AS k FROM prow_runs"),
			"prow_run_images":       rows("SELECT build_id AS k FROM prow_run_images"),
			"prow_install_failures": rows("SELECT build_id AS k FROM prow_install_failures"),
			"art_builds":            digests("art_builds"),
			"image_scans":           digests("image_scans"),
			"fbc_renders":           digests("fbc_renders"),
			"fbc_related_images":    digests("fbc_related_images"),
		}
	}
	before := state()

	deleted, err := d.Prune(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{
		"konflux_releases": 1, "konflux_release_art": 1, "snapshots": 1, "staged_snapshots": 1, "prow_runs": 2,
		"art_builds": 2, "image_scans": 2, "fbc_renders": 2, "fbc_related_images": 2,
	}
	if !maps.Equal(deleted, want) {
		t.Errorf("deleted = %v, want %v", deleted, want)
	}
	after := map[string]string{
		"snapshots":             "newest shipped staged young",
		"snapshot_components":   "newest shipped staged young",
		"staged_snapshots":      "staged",
		"konflux_releases":      "failed-staged failed-young progressing succeeded",
		"konflux_release_art":   "failed-staged failed-young progressing succeeded",
		"prow_runs":             "2 3 5",
		"prow_run_images":       "3",
		"prow_install_failures": "",
		"art_builds":            "catalog fresh history run3 staged",
		"image_scans":           "catalog fresh history run3 staged",
		"fbc_renders":           "catalog fresh history run3 staged",
		"fbc_related_images":    "catalog fresh history run3 staged",
	}
	got := state()
	for _, table := range slices.Sorted(maps.Keys(after)) {
		if got[table] != after[table] {
			t.Errorf("%s = %q, want %q (before %q)", table, got[table], after[table], before[table])
		}
	}

	if deleted, err := d.Prune(ctx, cutoff); err != nil || slices.ContainsFunc(slices.Collect(maps.Values(deleted)), func(n int64) bool { return n != 0 }) {
		t.Errorf("second Prune = %v, %v; want nothing deleted", deleted, err)
	}
}
