package db

import (
	"context"
	"database/sql"
	"errors"
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
			Version:     r.ReleaseVersion,
			Name:        r.Component,
			NVR:         r.Nvr,
			RecordID:    r.RecordID,
			UpstreamSHA: r.UpstreamSha,
			StartedAt:   parseTime(r.StartedAt),
		}
	}
	return builds, nil
}

func (d *DB) StoreArtBuildAttempts(ctx context.Context, group string, attempts []artbuild.Attempt, from, to time.Time) error {
	seen := to.UTC().Format(time.RFC3339)
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		for _, a := range attempts {
			if err := q.UpsertArtBuildAttempt(ctx, dbsqlc.UpsertArtBuildAttemptParams{
				GroupName:      group,
				RecordID:       a.RecordID,
				ReleaseVersion: a.Version,
				Component:      a.Name,
				Nvr:            a.NVR,
				Outcome:        a.Outcome,
				StartTime:      a.StartedAt.UTC().Format(time.RFC3339),
				FirstSeen:      seen,
				LastSeen:       seen,
			}); err != nil {
				return err
			}
		}
		return q.UpsertArtBuildCoverage(ctx, dbsqlc.UpsertArtBuildCoverageParams{
			GroupName:   group,
			CoveredFrom: from.UTC().Format(time.RFC3339),
			CoveredTo:   seen,
		})
	})
}

// ArtBuildCoverage returns when the span of build start times group's
// searches read without a gap begins; ok is false when it was never searched.
func (d *DB) ArtBuildCoverage(ctx context.Context, group string) (from time.Time, ok bool, err error) {
	s, err := d.queries().GetArtBuildCoverage(ctx, group)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseTime(s), true, nil
}

// ArtBuildAttempts returns version's image-build attempts in group inside
// the covered span, one per build, newest first.
func (d *DB) ArtBuildAttempts(ctx context.Context, group, version string) ([]artbuild.Attempt, error) {
	rows, err := d.queries().ListArtBuildAttempts(ctx, dbsqlc.ListArtBuildAttemptsParams{GroupName: group, ReleaseVersion: version})
	if err != nil {
		return nil, err
	}
	attempts := make([]artbuild.Attempt, len(rows))
	for i, r := range rows {
		attempts[i] = artbuild.Attempt{
			Version: version, Name: r.Component, NVR: r.Nvr,
			RecordID: r.RecordID, Outcome: r.Outcome, StartedAt: parseTime(r.StartTime),
		}
	}
	return attempts, nil
}
