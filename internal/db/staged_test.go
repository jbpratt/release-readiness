package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
