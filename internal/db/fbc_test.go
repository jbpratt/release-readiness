package db

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/fbc"
)

func TestFBCRenderCache(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	const (
		repo   = "quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc@"
		older  = repo + "sha256:0001"
		newer  = repo + "sha256:0002"
		ocp420 = repo + "sha256:0003"
		staged = repo + "sha256:0004"
		bundle = "registry.redhat.io/quay/quay-operator-bundle@sha256:b001"
	)
	for _, b := range []artbuild.HistoryBuild{
		{RecordID: "r1", NVR: "quay-operator-fbc-3.16.7-20261011100000.ocp4.21", Pullspec: older, StartTime: "2026-10-11T10:00:00"},
		{RecordID: "r2", NVR: "quay-operator-fbc-3.16.7-20261012100000.ocp4.21", Pullspec: newer, StartTime: "2026-10-12T10:00:00"},
		{RecordID: "r3", NVR: "quay-operator-fbc-3.16.7-20261011090000.ocp4.20", Pullspec: ocp420, StartTime: "2026-10-11T09:00:00"},
	} {
		b.Group, b.Type, b.Digest = "quay-3.16", artbuild.TypeFBC, b.Pullspec[len(repo):]
		if err := d.UpsertArtHistoryBuild(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	const snap = "quay-stage-3-16-7-fbc-20261012120000"
	if err := d.UpsertStagedSnapshot(ctx, snap, "3.16.7", "fbc", "stage"); err != nil {
		t.Fatal(err)
	}
	id, err := d.CreateSnapshot(ctx, "fbc-quay-3-16", snap, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateSnapshotComponent(ctx, id, "fbc-quay-3-16-quay-operator", staged); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	toRender := func(at time.Time) []string {
		t.Helper()
		got, err := d.FBCsToRender(ctx, at, 10)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Staged catalogs first, then the newest build of each slot.
	if got, want := toRender(now), []string{staged, newer, ocp420}; !slices.Equal(got, want) {
		t.Errorf("to render: got %v, want %v", got, want)
	}

	head := fbc.Channel{Name: "stable-3.16", HeadCSV: "quay-operator.v3.99.0-dev", HeadBundlePullspec: bundle, Replaces: "quay-operator.v3.16.6", SkipRange: ">=3.13.0 <3.99.0-dev"}
	rendered := fbc.Render{Digest: "sha256:0004", LabelNVR: "quay-operator-fbc-3.16.7-20261012110000.ocp4.21", Package: "quay-operator", Channel: head, State: fbc.StateRendered, RenderedAt: now}
	failed := fbc.Render{Digest: "sha256:0002", State: fbc.StateFailed, Error: "GET: 503", RenderedAt: now, RetryAfter: now.Add(time.Hour)}
	for _, r := range []fbc.Render{rendered, failed} {
		if err := d.UpsertFBCRender(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := toRender(now), []string{ocp420}; !slices.Equal(got, want) {
		t.Errorf("to render before the retry: got %v, want %v", got, want)
	}
	if got, want := toRender(now.Add(time.Hour)), []string{newer, ocp420}; !slices.Equal(got, want) {
		t.Errorf("to render at the retry: got %v, want %v", got, want)
	}

	// A rendered image is final: it stays rendered and unjoined.
	if err := d.UpsertFBCRender(ctx, fbc.Render{Digest: "sha256:0004", State: fbc.StateFailed, RenderedAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	unjoined, err := d.UnjoinedFBCRenders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unjoined) != 1 || unjoined[0].Digest != "sha256:0004" || unjoined[0].Channel.Name != "stable-3.16" || unjoined[0].Channel.HeadBundlePullspec != bundle {
		t.Errorf("unjoined: %+v", unjoined)
	}
	if err := d.UpsertArtHistoryBuild(ctx, artbuild.HistoryBuild{RecordID: "rb", Group: "quay-3.16", Type: artbuild.TypeBundle, NVR: "quay-operator-metadata-container-3.16.7.202610121915", Digest: "sha256:b001", Pullspec: "quay.io/x/art-images@sha256:b001"}); err != nil {
		t.Fatal(err)
	}
	if unjoined, err := d.UnjoinedFBCRenders(ctx); err != nil || len(unjoined) != 0 {
		t.Errorf("unjoined after the bundle is stored: %+v, %v", unjoined, err)
	}
}
