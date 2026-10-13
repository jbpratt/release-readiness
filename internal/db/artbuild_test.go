package db

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
)

func TestArtBuildCache(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	const (
		imgA = "quay.io/x/art-images@sha256:aaaa"
		imgB = "quay.io/x/art-images@sha256:bbbb"
	)
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// imgA is in both Snapshots, so it is first seen in the older one and
	// held by both applications.
	apps := []string{"quay-3-16", "quay-3-18"}
	for i, imgs := range [][]string{{imgA}, {imgA, imgB}} {
		id, err := d.CreateSnapshot(ctx, apps[i], "snap-"+string(rune('a'+i)), t0.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		for _, img := range imgs {
			if err := d.CreateSnapshotComponent(ctx, id, "quay-3-18-"+img[len(img)-4:], img); err != nil {
				t.Fatal(err)
			}
		}
	}

	candidates := func(retryBefore time.Time) []string {
		t.Helper()
		cands, err := d.ListArtBuildCandidates(ctx, retryBefore, 10)
		if err != nil {
			t.Fatal(err)
		}
		var images []string
		for _, c := range cands {
			images = append(images, c.Image)
			if c.Image == imgA && !c.FirstSeen.Equal(t0) {
				t.Errorf("imgA first seen %v, want %v", c.FirstSeen, t0)
			}
			if c.Image == imgA && !slices.Equal(slices.Sorted(slices.Values(c.Applications)), apps) {
				t.Errorf("imgA applications %v, want %v", c.Applications, apps)
			}
		}
		return images
	}

	now := t0.Add(48 * time.Hour)
	if got := candidates(now); !slices.Equal(got, []string{imgA, imgB}) && !slices.Equal(got, []string{imgB, imgA}) {
		t.Fatalf("candidates: got %v, want both images", got)
	}

	resolved := artbuild.Build{Digest: "sha256:aaaa", State: artbuild.StateResolved, NVR: "nvr-a", RecordID: "rec-a", CheckedAt: now}
	missed := artbuild.Build{Digest: "sha256:bbbb", State: artbuild.StateUnresolved, CheckedAt: now}
	for _, b := range []artbuild.Build{resolved, missed} {
		if err := d.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	// A resolved image is never looked up again; a miss only after its retry cutoff.
	if got := candidates(now.Add(-24 * time.Hour)); len(got) != 0 {
		t.Errorf("candidates within a day of the miss: got %v, want none", got)
	}
	if got := candidates(now.Add(time.Second)); !slices.Equal(got, []string{imgB}) {
		t.Errorf("candidates after the retry cutoff: got %v, want [%s]", got, imgB)
	}

	builds, err := d.ResolvedArtBuilds(ctx, []string{"sha256:aaaa", "sha256:bbbb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds["sha256:aaaa"] != resolved {
		t.Errorf("resolved builds: got %+v, want only %+v", builds, resolved)
	}
}

// An image held by a stage-staged image Snapshot is resolved ahead of a newer
// Snapshot's image.
func TestArtBuildCandidatesStagedFirst(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	const (
		staged = "quay.io/x/art-images@sha256:aaaa"
		newer  = "quay.io/x/art-images@sha256:bbbb"
	)
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, img := range []string{staged, newer} {
		id, err := d.CreateSnapshot(ctx, "quay-3-18", "snap-"+string(rune('a'+i)), t0.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err := d.CreateSnapshotComponent(ctx, id, "quay-3-18-"+img[len(img)-4:], img); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.UpsertStagedSnapshot(ctx, "snap-a", "3.18.1", "image", "stage"); err != nil {
		t.Fatal(err)
	}

	cands, err := d.ListArtBuildCandidates(ctx, t0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Image != staged {
		t.Errorf("candidates: got %+v, want only %s", cands, staged)
	}
}

// Each application's newest Snapshot images come before newer Snapshots of
// another application, so a quiet application is not starved on a fresh DB.
func TestArtBuildCandidatesNewestPerApplicationFirst(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	snap := func(app, name, img string, at time.Time) {
		t.Helper()
		id, err := d.CreateSnapshot(ctx, app, name, at)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.CreateSnapshotComponent(ctx, id, app, img); err != nil {
			t.Fatal(err)
		}
	}
	const imgY = "quay.io/x/y@sha256:yyyy"
	snap("quay-3-15", "y", imgY, t0)
	for i := range 3 {
		snap("quay-3-18", fmt.Sprintf("x-%d", i), fmt.Sprintf("quay.io/x/x@sha256:%04d", i), t0.Add(time.Duration(i+1)*time.Hour))
	}

	cands, err := d.ListArtBuildCandidates(ctx, t0, 2)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cands {
		got = append(got, c.Image)
	}
	if want := []string{"quay.io/x/x@sha256:0002", imgY}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestArtHistory(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	b := artbuild.HistoryBuild{
		RecordID: "r1", Group: "quay-3.17", Type: artbuild.TypeBundle,
		NVR:    "quay-operator-metadata-container-3.17.6.202610081142.p2.g3c4984a.assembly.stream.el9-1",
		Digest: "sha256:aaaa", Pullspec: "quay.io/x/art-images@sha256:aaaa", Commitish: "3c4984a",
	}
	for range 2 {
		if err := d.UpsertArtHistoryBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	for name, has := range map[string]func() (bool, error){
		"record": func() (bool, error) { return d.HasArtHistoryRecord(ctx, "r1") },
		"nvr":    func() (bool, error) { return d.HasArtHistoryNVR(ctx, b.Group, b.Type, b.NVR) },
		"digest": func() (bool, error) { return d.HasArtHistoryDigest(ctx, b.Type, b.Digest) },
	} {
		if ok, err := has(); err != nil || !ok {
			t.Errorf("has %s = %v, %v; want true", name, ok, err)
		}
	}
	if ok, err := d.HasArtHistoryNVR(ctx, b.Group, artbuild.TypeImage, b.NVR); err != nil || ok {
		t.Errorf("has the bundle NVR as an image = %v, %v; want false", ok, err)
	}
	images, err := d.ArtHistoryImages(ctx, []string{b.Digest, "sha256:none"})
	if want := (artbuild.HistoryBuild{RecordID: "r1", NVR: b.NVR, Digest: b.Digest, Commitish: "3c4984a"}); err != nil || len(images) != 1 || !reflect.DeepEqual(images[b.Digest], want) {
		t.Errorf("images %+v, %v; want only %+v", images, err, want)
	}

	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for _, day := range []string{"2026-10-11", "2026-10-12", "2026-10-12"} {
		if err := d.MarkArtHistoryDayScanned(ctx, "quay-3.17", artbuild.TypeFBC, day, at); err != nil {
			t.Fatal(err)
		}
	}
	days, err := d.ScannedArtHistoryDays(ctx, "quay-3.17", artbuild.TypeFBC)
	slices.Sort(days)
	if err != nil || !slices.Equal(days, []string{"2026-10-11", "2026-10-12"}) {
		t.Errorf("scanned days %q, %v", days, err)
	}
	if days, _ := d.ScannedArtHistoryDays(ctx, "quay-3.16", artbuild.TypeFBC); len(days) != 0 {
		t.Errorf("3.16 scanned days %q, want none", days)
	}

	for i, app := range []string{"quay-3-17", "fbc-quay-3-17", "quay-3-17"} {
		if _, err := d.CreateSnapshot(ctx, app, fmt.Sprint("snap-", i), at); err != nil {
			t.Fatal(err)
		}
	}
	if apps, err := d.ListSnapshotApplications(ctx); err != nil || !slices.Equal(apps, []string{"fbc-quay-3-17", "quay-3-17"}) {
		t.Errorf("applications %q, %v", apps, err)
	}
}
