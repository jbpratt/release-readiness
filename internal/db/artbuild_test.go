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
