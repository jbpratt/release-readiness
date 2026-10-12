package db

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertStagedSnapshot(ctx context.Context, name, assembly, kind, env string) error {
	return d.queries().UpsertStagedSnapshot(ctx, dbsqlc.UpsertStagedSnapshotParams{
		Name:     name,
		Assembly: assembly,
		Kind:     kind,
		Env:      env,
	})
}

var concreteVersion = regexp.MustCompile(`^quay-v((\d+)\.(\d+)\.\d+)$`)

// SelectedStageBuild returns the newest image Snapshot staged for release's
// concrete version whose Konflux Release succeeded through a ReleasePlan
// matching plans for the version's X-Y stream, ranked by that Release's
// completion time, so a newer pending or failed Release never displaces the
// last success. It returns nil when there is none.
func (d *DB) SelectedStageBuild(ctx context.Context, release *model.ReleaseVersion, plans *regexp.Regexp) (*model.SelectedBuild, error) {
	m := concreteVersion.FindStringSubmatch(release.Name)
	if m == nil || plans == nil {
		return nil, nil
	}
	rows, err := d.queries().ListSuccessfulStageReleases(ctx, dbsqlc.ListSuccessfulStageReleasesParams{
		Assembly:    m[1],
		Application: release.KonfluxApplication,
	})
	if err != nil {
		return nil, err
	}
	stream := "-" + m[2] + "-" + m[3]
	for _, r := range rows {
		if plans.MatchString(r.ReleasePlan) && strings.HasSuffix(r.ReleasePlan, stream) {
			return &model.SelectedBuild{SnapshotName: r.SnapshotName, CompletedAt: parseTime(r.CompletionTime)}, nil
		}
	}
	return nil, nil
}

// StageReleases returns the Releases of release's application through a
// ReleasePlan matching plans for the version's X-Y stream, the filter
// SelectedStageBuild applies, newest first, each with the component images of
// the Snapshot it names. A version that is not quay-vX.Y.Z has none.
func (d *DB) StageReleases(ctx context.Context, release *model.ReleaseVersion, plans *regexp.Regexp) ([]model.StageRelease, error) {
	m := concreteVersion.FindStringSubmatch(release.Name)
	if m == nil || plans == nil {
		return nil, nil
	}
	rows, err := d.queries().ListKonfluxReleasesByApplication(ctx, release.KonfluxApplication)
	if err != nil {
		return nil, err
	}
	imgs, err := d.queries().ListReleasedSnapshotImages(ctx, release.KonfluxApplication)
	if err != nil {
		return nil, err
	}
	images := map[string][]string{}
	for _, img := range imgs {
		images[img.Name] = append(images[img.Name], img.ImageUrl)
	}
	stream := "-" + m[2] + "-" + m[3]
	var releases []model.StageRelease
	for _, r := range rows {
		if plans.MatchString(r.ReleasePlan) && strings.HasSuffix(r.ReleasePlan, stream) {
			releases = append(releases, model.StageRelease{KonfluxRelease: toKonfluxRelease(r), Images: images[r.Snapshot]})
		}
	}
	return releases, nil
}

// StagedFBCCatalogs returns the catalogs of application's FBC Snapshots ART
// staged for stage for assembly, by Snapshot name, each with the newest
// Release of its Snapshot.
func (d *DB) StagedFBCCatalogs(ctx context.Context, assembly, application string) ([]model.CandidateCatalog, error) {
	rows, err := d.queries().ListStagedFBCCatalogs(ctx, dbsqlc.ListStagedFBCCatalogsParams{Assembly: assembly, Application: application})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	releases, err := d.queries().ListKonfluxReleasesByApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	catalogs := make([]model.CandidateCatalog, len(rows))
	for i, r := range rows {
		_, _, ocp, _ := artbuild.ParseFBC(r.Nvr)
		catalogs[i] = model.CandidateCatalog{State: "staged", Snapshot: r.SnapshotName, Component: r.Component, Pullspec: r.ImageUrl, NVR: r.Nvr, OCP: ocp}
		// releases is newest first.
		if j := slices.IndexFunc(releases, func(kr dbsqlc.KonfluxRelease) bool { return kr.Snapshot == r.SnapshotName }); j >= 0 {
			kr := toKonfluxRelease(releases[j])
			catalogs[i].Release, catalogs[i].ReleasePlan = &kr, kr.ReleasePlan
		}
	}
	return catalogs, nil
}

// LatestProdRelease returns the newest Release, whatever its status, of an
// image Snapshot ART staged for prod for assembly, or nil.
func (d *DB) LatestProdRelease(ctx context.Context, assembly string) (*model.KonfluxRelease, error) {
	row, err := d.queries().LatestProdImageRelease(ctx, assembly)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := toKonfluxRelease(row)
	return &r, nil
}
