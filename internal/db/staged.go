package db

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"slices"
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
// with the newest Release naming it, or nil when there is none.
func (d *DB) LatestStagedSnapshot(ctx context.Context, assembly, kind string) (*model.StagedSnapshot, error) {
	row, err := d.queries().LatestStagedSnapshot(ctx, dbsqlc.LatestStagedSnapshotParams{Assembly: assembly, Kind: kind})
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
	concreteVersion = regexp.MustCompile(`^quay-v(\d+\.\d+\.\d+)$`)
	imageDigest     = regexp.MustCompile(`@(sha256:[0-9a-f]{64})$`)
	fullSHA         = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// SelectedStageBuilds returns the two newest image Snapshots staged for
// release's concrete version whose Konflux Release through one of plans
// succeeded, ranked by that Release's completion time. Only an exact
// ReleasePlan name in plans counts as STAGE, so a newer pending or failed
// Release never displaces the last success.
func (d *DB) SelectedStageBuilds(ctx context.Context, release *model.ReleaseVersion, plans []string) (model.StageBuilds, error) {
	m := concreteVersion.FindStringSubmatch(release.Name)
	if m == nil {
		return model.StageBuilds{Reason: "not a concrete quay-vX.Y.Z version"}, nil
	}
	if len(plans) == 0 {
		return model.StageBuilds{Reason: "stage release plan not configured"}, nil
	}
	rows, err := d.queries().ListSuccessfulStageReleases(ctx, dbsqlc.ListSuccessfulStageReleasesParams{
		Assembly:     m[1],
		Application:  release.KonfluxApplication,
		ReleasePlans: plans,
	})
	if err != nil {
		return model.StageBuilds{}, err
	}
	var builds []*model.SelectedBuild
	for _, r := range rows {
		if len(builds) == 2 {
			break
		}
		// A Snapshot released again ranks by its newest success only.
		if slices.ContainsFunc(builds, func(b *model.SelectedBuild) bool { return b.SnapshotName == r.SnapshotName }) {
			continue
		}
		b, err := d.selectedBuild(ctx, release.Name, r)
		if err != nil {
			return model.StageBuilds{}, err
		}
		builds = append(builds, b)
	}
	if len(builds) == 0 {
		return model.StageBuilds{Reason: "no successful stage release of a staged image snapshot"}, nil
	}
	sb := model.StageBuilds{Selected: builds[0]}
	if len(builds) == 2 {
		sb.Previous = builds[1]
	}
	return sb, nil
}

func (d *DB) selectedBuild(ctx context.Context, version string, r dbsqlc.ListSuccessfulStageReleasesRow) (*model.SelectedBuild, error) {
	components, err := d.listSnapshotComponents(ctx, r.SnapshotID)
	if err != nil {
		return nil, err
	}
	b := &model.SelectedBuild{
		Version:           version,
		Stage:             "stage",
		SelectedAt:        parseTime(r.CompletionTime),
		ReleaseName:       r.ReleaseName,
		ReleasePlan:       r.ReleasePlan,
		SnapshotName:      r.SnapshotName,
		SnapshotCreatedAt: parseTime(r.SnapshotCreatedAt),
		Components:        make([]model.SelectedBuildComponent, len(components)),
		RRURL:             "/releases/" + version + "/snapshots?snapshot=" + url.QueryEscape(r.SnapshotName),
	}
	var digests []string
	for i, c := range components {
		b.Components[i] = model.SelectedBuildComponent{Name: c.Component, ProvenanceState: "unknown"}
		if m := imageDigest.FindStringSubmatch(c.ImageURL); m != nil {
			b.Components[i].ImageDigest = m[1]
			digests = append(digests, m[1])
		}
	}
	art, err := d.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	for i := range b.Components {
		c := &b.Components[i]
		a, ok := art[c.ImageDigest]
		if !ok {
			continue
		}
		c.UpstreamRepo, c.UpstreamSHA = a.UpstreamRepo, a.UpstreamSHA
		if a.UpstreamRepo != "" && fullSHA.MatchString(a.UpstreamSHA) {
			c.ProvenanceState = "resolved"
		}
	}
	return b, nil
}
