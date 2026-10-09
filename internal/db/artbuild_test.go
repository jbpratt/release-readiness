package db

import (
	"path/filepath"
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

	resolved := artbuild.Build{Digest: "sha256:aaaa", State: artbuild.StateResolved, NVR: "nvr-a", RecordID: "rec-a", UpstreamSHA: "abc", CheckedAt: now}
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

func TestArtPendingBuilds(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	t0 := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	build := func(version, name, rec string) artbuild.PendingBuild {
		return artbuild.PendingBuild{Version: version, Name: name, NVR: name + "-" + version + "-1", RecordID: rec, UpstreamSHA: "sha-" + rec, StartedAt: t0}
	}
	quay := build("3.18.1", "quay-quay-container", "a")
	clair := build("3.18.1", "quay-clair-container", "b")
	old := build("3.16.3", "quay-quay-container", "c")
	for group, builds := range map[string][]artbuild.PendingBuild{"quay-3.18": {quay, clair}, "quay-3.16": {old}} {
		if err := d.ReplaceArtPendingBuilds(ctx, group, builds, t0); err != nil {
			t.Fatal(err)
		}
	}
	// A later search of one group replaces only that group's set.
	if err := d.ReplaceArtPendingBuilds(ctx, "quay-3.18", []artbuild.PendingBuild{quay}, t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got, err := d.ArtPendingBuilds(ctx, []string{"3.18.1", "3.16.3"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !slices.Contains(got, quay) || !slices.Contains(got, old) {
		t.Errorf("pending builds: got %+v, want %+v and %+v", got, quay, old)
	}
	// Rows from a search before checkedSince are stale.
	if got, err := d.ArtPendingBuilds(ctx, []string{"3.18.1", "3.16.3"}, t0.Add(time.Minute)); err != nil || !slices.Equal(got, []artbuild.PendingBuild{quay}) {
		t.Errorf("fresh pending builds: got %+v, %v; want only %+v", got, err, quay)
	}
}

func TestArtBuildAttempts(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	const (
		group = "quay-3.18"
		quay  = "quay-quay-container-3.18.1-202610010000.p2.gaaaaaaa.assembly.stream.el9"
		clair = "quay-clair-container-3.18.1-202610020000.p2.gbbbbbbb.assembly.stream.el9"
	)
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	attempt := func(nvr, rec, outcome string, started time.Time) artbuild.Attempt {
		name, version := artbuild.SplitNVR(nvr)
		return artbuild.Attempt{Version: version, Name: name, NVR: nvr, RecordID: rec, Outcome: outcome, StartedAt: started}
	}
	store := func(from, to time.Time, attempts ...artbuild.Attempt) {
		t.Helper()
		if err := d.StoreArtBuildAttempts(ctx, group, attempts, from, to); err != nil {
			t.Fatal(err)
		}
	}
	list := func() []string {
		t.Helper()
		attempts, err := d.ArtBuildAttempts(ctx, group, "3.18.1")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, a := range attempts {
			got = append(got, a.RecordID+":"+a.Outcome)
		}
		return got
	}

	if _, ok, err := d.ArtBuildCoverage(ctx, group); ok || err != nil {
		t.Fatalf("coverage before any search: ok %v, err %v; want none", ok, err)
	}

	// clair's pending record is updated in place; quay's finishes as a
	// second record of its NVR, which replaces the pending one.
	store(t0.Add(-7*24*time.Hour), t0.Add(time.Hour),
		attempt(quay, "q-pending", "pending", t0),
		attempt(clair, "c-1", "pending", t0.Add(30*time.Minute)))
	if got, want := list(), []string{"c-1:pending", "q-pending:pending"}; !slices.Equal(got, want) {
		t.Errorf("first pass: got %v, want %v", got, want)
	}
	store(t0.Add(-6*24*time.Hour), t0.Add(2*time.Hour),
		attempt(quay, "q-pending", "pending", t0),
		attempt(quay, "q-done", "success", t0.Add(time.Minute)),
		attempt(clair, "c-1", "build_error", t0.Add(30*time.Minute)))
	if got, want := list(), []string{"c-1:build_error", "q-done:success"}; !slices.Equal(got, want) {
		t.Errorf("second pass: got %v, want %v", got, want)
	}
	from, ok, err := d.ArtBuildCoverage(ctx, group)
	if err != nil || !ok || !from.Equal(t0.Add(-7*24*time.Hour)) {
		t.Errorf("coverage: from %v ok %v err %v; want both windows joined", from, ok, err)
	}

	// A window starting after the covered span ends leaves a gap: the span
	// restarts, and attempts before it are no longer known complete.
	gapFrom := t0.Add(10 * 24 * time.Hour)
	store(gapFrom, gapFrom.Add(7*24*time.Hour), attempt(quay, "q-late", "build_error", gapFrom.Add(time.Hour)))
	if from, _, _ := d.ArtBuildCoverage(ctx, group); !from.Equal(gapFrom) {
		t.Errorf("coverage after a gap starts %v, want %v", from, gapFrom)
	}
	if got, want := list(), []string{"q-late:build_error"}; !slices.Equal(got, want) {
		t.Errorf("after a gap: got %v, want %v", got, want)
	}
}
