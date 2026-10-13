package db

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

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
// Release of its Snapshot. ART stages a catalog digest again in each new
// Snapshot, so each digest appears once: from the Snapshot with the newest
// Release, else the newest Snapshot.
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
	// rank is the index in releases of each catalog's Release, len(releases)
	// for none; best is the catalog kept for each digest.
	rank, best := make([]int, len(rows)), map[string]int{}
	for i, r := range rows {
		_, _, ocp, _ := artbuild.ParseFBC(r.Nvr)
		catalogs[i] = model.CandidateCatalog{State: "staged", Snapshot: r.SnapshotName, Component: r.Component, Pullspec: r.ImageUrl, NVR: r.Nvr, OCP: ocp}
		// releases is newest first.
		rank[i] = slices.IndexFunc(releases, func(kr dbsqlc.KonfluxRelease) bool { return kr.Snapshot == r.SnapshotName })
		if rank[i] >= 0 {
			kr := toKonfluxRelease(releases[rank[i]])
			catalogs[i].Release, catalogs[i].ReleasePlan = &kr, kr.ReleasePlan
		} else {
			rank[i] = len(releases)
		}
		_, digest, _ := strings.Cut(r.ImageUrl, "@")
		if b, ok := best[digest]; !ok || rank[i] < rank[b] || rank[i] == rank[b] && r.SnapshotName > rows[b].SnapshotName {
			best[digest] = i
		}
	}
	kept := catalogs[:0]
	for i, c := range catalogs {
		if _, digest, _ := strings.Cut(c.Pullspec, "@"); best[digest] == i {
			kept = append(kept, c)
		}
	}
	return kept, nil
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

// StagedFBCMembers returns the catalogs of application's FBC Snapshots ART
// staged for stage for assembly, newest Snapshot first, each with the newest
// Release of its Snapshot, whatever its status.
func (d *DB) StagedFBCMembers(ctx context.Context, assembly, application string) ([]model.FBCMember, error) {
	rows, err := d.queries().ListStagedFBCMembers(ctx, dbsqlc.ListStagedFBCMembersParams{Assembly: assembly, Application: application})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	releases, err := d.queries().ListKonfluxReleasesByApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	members := make([]model.FBCMember, len(rows))
	for i, r := range rows {
		_, digest, _ := strings.Cut(r.ImageUrl, "@")
		members[i] = model.FBCMember{
			Digest: digest, Pullspec: r.ImageUrl, NVR: r.Nvr, HeadBundleNVR: r.HeadBundleNvr, Snapshot: r.SnapshotName,
			RecordID: r.RecordID, Channel: r.Channel, HeadCSV: r.HeadCsv, PreviousVersion: r.PreviousVersion,
			ReadError: r.ReadError,
		}
		// releases is newest first.
		if j := slices.IndexFunc(releases, func(kr dbsqlc.KonfluxRelease) bool { return kr.Snapshot == r.SnapshotName }); j >= 0 {
			kr := toKonfluxRelease(releases[j])
			members[i].Release = &kr
		}
	}
	return members, nil
}

// ArtHistoryFBCs returns group's successful ART FBC builds, newest NVR first.
func (d *DB) ArtHistoryFBCs(ctx context.Context, group string) ([]model.FBCMember, error) {
	rows, err := d.queries().ListArtHistoryFBCs(ctx, group)
	if err != nil {
		return nil, err
	}
	members := make([]model.FBCMember, len(rows))
	for i, r := range rows {
		_, digest, _ := strings.Cut(r.Pullspec, "@")
		members[i] = model.FBCMember{
			Digest: digest, Pullspec: r.Pullspec, NVR: r.Nvr, HeadBundleNVR: r.HeadBundleNvr,
			RecordID: r.RecordID, Channel: r.Channel, HeadCSV: r.HeadCsv, PreviousVersion: r.PreviousVersion,
		}
	}
	return members, nil
}

// FBCHistory returns group's successful ART FBC builds of assembly, newest
// first, "shipped" or "staged" by the Releases of application's FBC Snapshots
// ART staged for assembly. Runs is unset.
func (d *DB) FBCHistory(ctx context.Context, group, assembly, application string) ([]model.FBCBuild, error) {
	rows, err := d.queries().ListFBCHistory(ctx, dbsqlc.ListFBCHistoryParams{GroupName: group, Assembly: assembly, Application: application})
	if err != nil {
		return nil, err
	}
	builds := []model.FBCBuild{}
	for _, r := range rows {
		// The group holds every z of the stream.
		operator, version, ocp, ok := artbuild.ParseFBC(r.Nvr)
		if !ok || version != assembly {
			continue
		}
		b := model.FBCBuild{Operator: operator, OCP: ocp, NVR: r.Nvr, Digest: r.Digest}
		// ART gives the start time in UTC with no zone.
		if t, err := time.Parse("2006-01-02T15:04:05", r.StartTime); err == nil {
			b.BuiltAt = &t
		}
		switch {
		case r.Shipped:
			b.State = "shipped"
		case r.Staged:
			b.State = "staged"
		}
		builds = append(builds, b)
	}
	return builds, nil
}

// ProdReleasedImages returns the images of application's image Snapshots ART
// staged for prod that a Release shipped.
func (d *DB) ProdReleasedImages(ctx context.Context, application string) ([]string, error) {
	return d.queries().ListProdReleasedImages(ctx, application)
}
