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
		Digest:       b.Digest,
		State:        b.State,
		Nvr:          b.NVR,
		RecordID:     b.RecordID,
		UpstreamRepo: b.UpstreamRepo,
		UpstreamSha:  b.UpstreamSHA,
		RebaseRepo:   b.RebaseRepo,
		RebaseSha:    b.RebaseSHA,
		PipelineUrl:  b.PipelineURL,
		CheckedAt:    b.CheckedAt.UTC().Format(time.RFC3339),
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
			Digest:       r.Digest,
			State:        r.State,
			NVR:          r.Nvr,
			RecordID:     r.RecordID,
			UpstreamRepo: r.UpstreamRepo,
			UpstreamSHA:  r.UpstreamSha,
			RebaseRepo:   r.RebaseRepo,
			RebaseSHA:    r.RebaseSha,
			PipelineURL:  r.PipelineUrl,
			CheckedAt:    parseTime(r.CheckedAt),
		}
	}
	return builds, nil
}

func (d *DB) ListActiveApplications(ctx context.Context) ([]string, error) {
	return d.queries().ListActiveApplications(ctx)
}

func (d *DB) ReplaceArtPendingBuilds(ctx context.Context, group string, builds []artbuild.PendingBuild, checkedAt time.Time) error {
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		if err := q.DeleteArtPendingBuilds(ctx, group); err != nil {
			return err
		}
		for _, b := range builds {
			if err := q.InsertArtPendingBuild(ctx, dbsqlc.InsertArtPendingBuildParams{
				GroupName:      group,
				ReleaseVersion: b.Version,
				Component:      b.Name,
				Nvr:            b.NVR,
				RecordID:       b.RecordID,
				UpstreamSha:    b.UpstreamSHA,
				UpstreamRepo:   b.UpstreamRepo,
				StartedAt:      b.StartedAt.UTC().Format(time.RFC3339),
				CheckedAt:      checkedAt.UTC().Format(time.RFC3339),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ArtPendingBuilds returns the pending builds of versions stored by a search
// at or after checkedSince.
func (d *DB) ArtPendingBuilds(ctx context.Context, versions []string, checkedSince time.Time) ([]artbuild.PendingBuild, error) {
	if len(versions) == 0 {
		return nil, nil
	}
	rows, err := d.queries().ListArtPendingBuilds(ctx, dbsqlc.ListArtPendingBuildsParams{
		CheckedAt: checkedSince.UTC().Format(time.RFC3339),
		Versions:  versions,
	})
	if err != nil {
		return nil, err
	}
	builds := make([]artbuild.PendingBuild, len(rows))
	for i, r := range rows {
		builds[i] = artbuild.PendingBuild{
			Group:        r.GroupName,
			Version:      r.ReleaseVersion,
			Name:         r.Component,
			NVR:          r.Nvr,
			RecordID:     r.RecordID,
			UpstreamRepo: r.UpstreamRepo,
			UpstreamSHA:  r.UpstreamSha,
			StartedAt:    parseTime(r.StartedAt),
		}
	}
	return builds, nil
}
