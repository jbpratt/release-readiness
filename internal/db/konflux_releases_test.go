package db

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/model"
)

func TestKonfluxReleaseArt(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	r := model.KonfluxRelease{
		Name: "rel", Application: "quay-3-18", Snapshot: "snap", ReleasePlan: "quay-advisory-stage-3-18",
		ReleasedStatus: "True", ReleasedReason: "Succeeded", CreatedAt: time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC),
		JobURL: "https://art-jenkins.example/job/1", Advisory: "https://access.stage.redhat.com/errata/RHBA-2026:153498",
	}
	if err := d.UpsertKonfluxRelease(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertStagedSnapshot(ctx, "snap", "3.18.1", "image", "prod"); err != nil {
		t.Fatal(err)
	}

	all, err := d.KonfluxReleases(ctx)
	if err != nil || all["rel"] != r {
		t.Errorf("KonfluxReleases = %+v, %v; want %+v", all["rel"], err, r)
	}
	v := &model.ReleaseVersion{Name: "quay-v3.18.1", KonfluxApplication: "quay-3-18"}
	staged, err := d.StageReleases(ctx, v, regexp.MustCompile(`^quay-advisory-stage-\d+-\d+$`))
	if err != nil || len(staged) != 1 || staged[0].KonfluxRelease != r {
		t.Errorf("StageReleases = %+v, %v; want %+v", staged, err, r)
	}
	prod, err := d.LatestProdRelease(ctx, "3.18.1")
	if err != nil || prod == nil || *prod != r {
		t.Errorf("LatestProdRelease = %+v, %v; want %+v", prod, err, r)
	}
}
