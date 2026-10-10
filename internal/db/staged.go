package db

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) UpsertStagedSnapshot(ctx context.Context, name, assembly, kind, env string, createdAt time.Time) error {
	return d.queries().UpsertStagedSnapshot(ctx, dbsqlc.UpsertStagedSnapshotParams{
		Name:      name,
		Assembly:  assembly,
		Kind:      kind,
		Env:       env,
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
	})
}

// LatestStagedSnapshot returns the newest stage Snapshot of kind for assembly
// holding component, or of any component when it is empty, with the newest
// Release naming it, or nil when there is none.
func (d *DB) LatestStagedSnapshot(ctx context.Context, assembly, kind, component string) (*model.StagedSnapshot, error) {
	row, err := d.queries().LatestStagedSnapshot(ctx, dbsqlc.LatestStagedSnapshotParams{Assembly: assembly, Kind: kind, Component: component})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := &model.StagedSnapshot{Name: row.Name, CreatedAt: parseTime(row.CreatedAt)}
	releases, err := d.queries().ListKonfluxReleasesBySnapshots(ctx, []string{row.Name})
	if err != nil {
		return nil, err
	}
	if len(releases) > 0 {
		r := toKonfluxRelease(releases[0])
		s.Release = &r
	}
	return s, nil
}

var (
	concreteVersion = regexp.MustCompile(`^quay-v((\d+)\.(\d+)\.\d+)$`)
	imageDigest     = regexp.MustCompile(`@(sha256:[0-9a-f]{64})$`)
)

// StreamStaged reports whether ART staged a Snapshot for any version of the
// X.Y stream of name, e.g. 3.18.0 for quay-v3.18.1. A name that is not
// quay-vX.Y.Z has none.
func (d *DB) StreamStaged(ctx context.Context, name string) (bool, error) {
	m := concreteVersion.FindStringSubmatch(name)
	if m == nil {
		return false, nil
	}
	return d.queries().StreamStaged(ctx, m[2]+"."+m[3])
}

// SelectedStageBuild returns the newest image Snapshot staged for release's
// concrete version whose Konflux Release succeeded through a ReleasePlan
// matching plans for the version's X-Y stream, ranked by that Release's
// completion time, so a newer pending or failed Release never displaces the
// last success. It returns nil and the reason when there is none.
func (d *DB) SelectedStageBuild(ctx context.Context, release *model.ReleaseVersion, plans *regexp.Regexp) (*model.SelectedBuild, string, error) {
	m := concreteVersion.FindStringSubmatch(release.Name)
	if m == nil {
		return nil, "not a concrete quay-vX.Y.Z version", nil
	}
	if plans == nil {
		return nil, "stage release plan not configured", nil
	}
	rows, err := d.queries().ListSuccessfulStageReleases(ctx, dbsqlc.ListSuccessfulStageReleasesParams{
		Assembly:    m[1],
		Application: release.KonfluxApplication,
	})
	if err != nil {
		return nil, "", err
	}
	stream := "-" + m[2] + "-" + m[3]
	for _, r := range rows {
		if plans.MatchString(r.ReleasePlan) && strings.HasSuffix(r.ReleasePlan, stream) {
			b, err := d.selectedBuild(ctx, r)
			return b, "", err
		}
	}
	return nil, "no successful stage release of a staged image snapshot", nil
}

func (d *DB) selectedBuild(ctx context.Context, r dbsqlc.ListSuccessfulStageReleasesRow) (*model.SelectedBuild, error) {
	components, err := d.listSnapshotComponents(ctx, r.SnapshotID)
	if err != nil {
		return nil, err
	}
	b := &model.SelectedBuild{
		SnapshotName: r.SnapshotName,
		CompletedAt:  parseTime(r.CompletionTime),
		Components:   make([]model.SelectedBuildComponent, len(components)),
	}
	digests := make([]string, len(components))
	for i, c := range components {
		b.Components[i].Name = c.Component
		if m := imageDigest.FindStringSubmatch(c.ImageURL); m != nil {
			digests[i] = m[1]
		}
	}
	art, err := d.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	for i, digest := range digests {
		if a, ok := art[digest]; ok {
			b.Components[i].UpstreamRepo, b.Components[i].UpstreamSHA = a.UpstreamRepo, a.UpstreamSHA
		}
	}
	return b, nil
}
