package db

import (
	"context"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/db/sqlc"
)

func (d *DB) UpsertArtBuild(ctx context.Context, b artbuild.Build) error {
	return d.queries().UpsertArtBuild(ctx, dbsqlc.UpsertArtBuildParams{
		Digest:    b.Digest,
		State:     b.State,
		Nvr:       b.NVR,
		RecordID:  b.RecordID,
		CheckedAt: b.CheckedAt.UTC().Format(time.RFC3339),
	})
}

// ListArtBuildCandidates returns up to limit stored component images with no
// ART record, or whose miss was checked before retryBefore.
func (d *DB) ListArtBuildCandidates(ctx context.Context, retryBefore time.Time, limit int) ([]artbuild.Candidate, error) {
	rows, err := d.queries().ListArtBuildCandidates(ctx, dbsqlc.ListArtBuildCandidatesParams{
		CheckedAt: retryBefore.UTC().Format(time.RFC3339),
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, err
	}
	cands := make([]artbuild.Candidate, len(rows))
	for i, r := range rows {
		cands[i] = artbuild.Candidate{Image: r.ImageUrl, Applications: strings.Split(r.Applications, ","), Component: r.Component, FirstSeen: parseTime(r.FirstSeen)}
	}
	return cands, nil
}

// ResolvedArtBuilds returns the resolved ART builds among digests, by digest.
func (d *DB) ResolvedArtBuilds(ctx context.Context, digests []string) (map[string]artbuild.Build, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	rows, err := d.queries().ListResolvedArtBuilds(ctx, digests)
	if err != nil {
		return nil, err
	}
	builds := make(map[string]artbuild.Build, len(rows))
	for _, r := range rows {
		builds[r.Digest] = artbuild.Build{
			Digest:    r.Digest,
			State:     r.State,
			NVR:       r.Nvr,
			RecordID:  r.RecordID,
			CheckedAt: parseTime(r.CheckedAt),
		}
	}
	return builds, nil
}

func (d *DB) ListSnapshotApplications(ctx context.Context) ([]string, error) {
	return d.queries().ListSnapshotApplications(ctx)
}

func (d *DB) ScannedArtHistoryDays(ctx context.Context, group, buildType string) ([]string, error) {
	return d.queries().ListArtHistoryScanDays(ctx, dbsqlc.ListArtHistoryScanDaysParams{GroupName: group, BuildType: buildType})
}

func (d *DB) MarkArtHistoryDayScanned(ctx context.Context, group, buildType, day string, at time.Time) error {
	return d.queries().UpsertArtHistoryScanDay(ctx, dbsqlc.UpsertArtHistoryScanDayParams{
		GroupName: group,
		BuildType: buildType,
		Day:       day,
		CheckedAt: at.UTC().Format(time.RFC3339),
	})
}

func (d *DB) HasArtHistoryRecord(ctx context.Context, recordID string) (bool, error) {
	n, err := d.queries().HasArtHistoryRecord(ctx, recordID)
	return n == 1, err
}

func (d *DB) HasArtHistoryNVR(ctx context.Context, group, buildType, nvr string) (bool, error) {
	n, err := d.queries().HasArtHistoryNVR(ctx, dbsqlc.HasArtHistoryNVRParams{GroupName: group, BuildType: buildType, Nvr: nvr})
	return n == 1, err
}

func (d *DB) HasArtHistoryDigest(ctx context.Context, buildType, digest string) (bool, error) {
	n, err := d.queries().HasArtHistoryDigest(ctx, dbsqlc.HasArtHistoryDigestParams{BuildType: buildType, Digest: digest})
	return n == 1, err
}

// UpsertArtHistoryBuild stores a successful ART build record.
func (d *DB) UpsertArtHistoryBuild(ctx context.Context, b artbuild.HistoryBuild) error {
	return d.queries().UpsertArtHistoryBuild(ctx, dbsqlc.UpsertArtHistoryBuildParams{
		RecordID:  b.RecordID,
		GroupName: b.Group,
		BuildType: b.Type,
		Nvr:       b.NVR,
		Digest:    b.Digest,
		Pullspec:  b.Pullspec,
		Commitish: b.Commitish,
		StartTime: b.StartTime,
	})
}

// ArtHistoryImages returns the stored ART bundle and image builds among
// digests, by digest; RecordID, NVR, Digest and Commitish are set.
func (d *DB) ArtHistoryImages(ctx context.Context, digests []string) (map[string]artbuild.HistoryBuild, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	rows, err := d.queries().ListArtHistoryImages(ctx, digests)
	if err != nil {
		return nil, err
	}
	builds := make(map[string]artbuild.HistoryBuild, len(rows))
	for _, r := range rows {
		if _, ok := builds[r.Digest]; !ok {
			builds[r.Digest] = artbuild.HistoryBuild{RecordID: r.RecordID, NVR: r.Nvr, Digest: r.Digest, Commitish: r.Commitish}
		}
	}
	return builds, nil
}

// UpsertArtSourceRepo stores the name and source repo of a record; "" marks
// it checked with none.
func (d *DB) UpsertArtSourceRepo(ctx context.Context, recordID, name, sourceRepo string) error {
	return d.queries().UpsertArtSourceRepo(ctx, dbsqlc.UpsertArtSourceRepoParams{RecordID: recordID, Name: name, SourceRepo: sourceRepo})
}

// ArtHistoryWithoutSource returns up to limit stored records never checked
// for a source repo, newest first; RecordID, NVR and Pullspec are set.
func (d *DB) ArtHistoryWithoutSource(ctx context.Context, limit int) ([]artbuild.HistoryBuild, error) {
	rows, err := d.queries().ListArtHistoryWithoutSource(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	builds := make([]artbuild.HistoryBuild, len(rows))
	for i, r := range rows {
		builds[i] = artbuild.HistoryBuild{RecordID: r.RecordID, NVR: r.Nvr, Pullspec: r.Pullspec}
	}
	return builds, nil
}
