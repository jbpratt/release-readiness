package db

import (
	"maps"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
)

// An application's stream builds leave out ART's assemblies and fbc-ri-*
// re-releases, newest first. Only an image whose ART record is resolved with
// an NVR has one.
func TestStreamBuilds(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	t0 := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	image := func(digest string) string { return "quay.io/x/art-images@sha256:" + digest }
	snapshot := func(app, name string, hours int, images map[string]string) {
		t.Helper()
		id, err := d.CreateSnapshot(ctx, app, name, t0.Add(time.Duration(hours)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		for c, img := range images {
			if err := d.CreateSnapshotComponent(ctx, id, c, img); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshot("quay-3-18", "quay-3-18-a", 0, map[string]string{"quay-3-18-quay-quay": image("aaaa"), "quay-3-18-quay-clair": image("cccc")})
	snapshot("quay-3-18", "quay-3-18-b", 1, map[string]string{"quay-3-18-quay-quay": image("bbbb"), "quay-3-18-quay-clair": image("cccc"), "quay-3-18-quay-operator": image("dddd")})
	snapshot("quay-3-18", "fbc-ri-stage-quay-3-18-quay-operator-x", 2, map[string]string{"quay-3-18-quay-quay": image("bbbb")})
	snapshot("quay-3-18", "quay-stage-3-18-1-image-x", 3, map[string]string{"quay-3-18-quay-quay": image("bbbb")})
	if err := d.UpsertStagedSnapshot(ctx, "quay-stage-3-18-1-image-x", "3.18.1", "image", "stage", t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	snapshot("quay-3-17", "quay-3-17-a", 4, map[string]string{"quay-3-17-quay-quay": image("eeee")})
	for _, b := range []artbuild.Build{
		{Digest: "sha256:bbbb", State: artbuild.StateResolved, NVR: "quay-quay-container-3.18.1-202610092112.p2.g66fc606.assembly.stream.el9"},
		{Digest: "sha256:cccc", State: artbuild.StateUnresolved},
		{Digest: "sha256:dddd", State: artbuild.StateResolved},
	} {
		b.CheckedAt = t0
		if err := d.UpsertArtBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	builds, nvrs, err := d.StreamBuilds(ctx, "quay-3-18")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ReleaseSnapshot{
		{Application: "quay-3-18", Name: "quay-3-18-b", CreatedAt: t0.Add(time.Hour), Components: []model.SnapshotImage{
			{Name: "quay-3-18-quay-clair", Image: image("cccc")},
			{Name: "quay-3-18-quay-operator", Image: image("dddd")},
			{Name: "quay-3-18-quay-quay", Image: image("bbbb")},
		}},
		{Application: "quay-3-18", Name: "quay-3-18-a", CreatedAt: t0, Components: []model.SnapshotImage{
			{Name: "quay-3-18-quay-clair", Image: image("cccc")},
			{Name: "quay-3-18-quay-quay", Image: image("aaaa")},
		}},
	}
	if !reflect.DeepEqual(builds, want) {
		t.Errorf("builds = %+v, want %+v", builds, want)
	}
	wantNVRs := map[string]string{image("bbbb"): "quay-quay-container-3.18.1-202610092112.p2.g66fc606.assembly.stream.el9"}
	if !maps.Equal(nvrs, wantNVRs) {
		t.Errorf("nvrs = %v, want %v", nvrs, wantNVRs)
	}
}
