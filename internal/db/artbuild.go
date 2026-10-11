package db

import (
	"context"
	"encoding/json"
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

// UpsertFBCRecord stores what the ART build record of an FBC image says of it.
func (d *DB) UpsertFBCRecord(ctx context.Context, r artbuild.FBCRecord) error {
	// A string slice cannot fail to marshal.
	bundles, _ := json.Marshal(r.BundleNVRs)
	return d.queries().UpsertArtFBCRecord(ctx, dbsqlc.UpsertArtFBCRecordParams{
		Digest:     r.Digest,
		Pullspec:   r.Pullspec,
		BundleNvrs: string(bundles),
		EndTime:    r.EndTime,
	})
}

// ListUnreadFBCImages returns up to limit resolved FBC images without an FBC
// record.
func (d *DB) ListUnreadFBCImages(ctx context.Context, limit int) ([]artbuild.Build, error) {
	rows, err := d.queries().ListUnreadArtFBCImages(ctx)
	if err != nil {
		return nil, err
	}
	var builds []artbuild.Build
	for _, r := range rows {
		if _, _, _, ok := artbuild.ParseFBC(r.Nvr); ok && len(builds) < limit {
			builds = append(builds, artbuild.Build{Digest: r.Digest, State: artbuild.StateResolved, NVR: r.Nvr, RecordID: r.RecordID})
		}
	}
	return builds, nil
}

// FBCImages returns the resolved FBC images of X.Y.Z version, each with its
// FBC record, nil until read.
func (d *DB) FBCImages(ctx context.Context, version string) ([]artbuild.FBCImage, error) {
	rows, err := d.queries().ListArtFBCImages(ctx)
	if err != nil {
		return nil, err
	}
	var images []artbuild.FBCImage
	for _, r := range rows {
		operator, v, ocp, ok := artbuild.ParseFBC(r.Nvr)
		if !ok || v != version {
			continue
		}
		img := artbuild.FBCImage{NVR: r.Nvr, Operator: operator, OCP: ocp}
		if r.Pullspec.Valid {
			img.Record = &artbuild.FBCRecord{Digest: r.Digest, Pullspec: r.Pullspec.String, EndTime: r.EndTime.String}
			if err := json.Unmarshal([]byte(r.BundleNvrs.String), &img.Record.BundleNVRs); err != nil {
				return nil, err
			}
		}
		images = append(images, img)
	}
	return images, nil
}
