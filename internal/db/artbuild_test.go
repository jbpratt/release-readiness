package db

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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

// The FBC images the resolver reads ART's records of: resolved, with an FBC
// NVR, without an FBC record, at most limit.
func TestListUnreadFBCImages(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	unread := []artbuild.Build{
		{Digest: "sha256:aaaa", State: artbuild.StateResolved, NVR: "quay-operator-fbc-3.18.1-20261009044841.ocp4.21", RecordID: "rec-a"},
		{Digest: "sha256:bbbb", State: artbuild.StateResolved, NVR: "quay-bridge-operator-fbc-3.18.1-20261009044154.ocp4.12", RecordID: "rec-b"},
	}
	for _, b := range append([]artbuild.Build{
		{Digest: "sha256:cccc", State: artbuild.StateResolved, NVR: "container-security-operator-fbc-3.18.1-20261009044154.ocp5.0", RecordID: "rec-c"},
		{Digest: "sha256:dddd", State: artbuild.StateResolved, NVR: "quay-operator-container-3.18.1-202609300827.p2.g35cf767.assembly.stream.el9", RecordID: "rec-d"},
		{Digest: "sha256:eeee", State: artbuild.StateResolved, NVR: "quay-operator-fbc-index-3.18.1", RecordID: "rec-e"},
		{Digest: "sha256:ffff", State: artbuild.StateUnresolved},
	}, unread...) {
		b.CheckedAt = time.Now()
		if err := d.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.UpsertFBCRecord(ctx, artbuild.FBCRecord{Digest: "sha256:cccc", Pullspec: "quay.io/x/art-fbc@sha256:cccc", EndTime: "2026-10-09T05:42:26", BundleNVRs: []string{}}); err != nil {
		t.Fatal(err)
	}

	got, err := d.ListUnreadFBCImages(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b artbuild.Build) int { return strings.Compare(a.Digest, b.Digest) })
	if !slices.Equal(got, unread) {
		t.Errorf("unread FBC images = %+v, want %+v", got, unread)
	}
	if got, err := d.ListUnreadFBCImages(ctx, 1); err != nil || len(got) != 1 || !slices.Contains(unread, got[0]) {
		t.Errorf("unread FBC images, at most 1 = %+v, %v; want one of %+v", got, err, unread)
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
