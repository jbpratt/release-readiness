package db

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/fbc"
)

// FBCsToRender returns up to limit pullspecs of FBC images to render: the
// catalogs of staged FBC Snapshots, then the newest ART FBC build of each
// operator, version and OCP, each newest first. It skips rendered images and
// failed ones to retry after now.
func (d *DB) FBCsToRender(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := d.queries().ListFBCRenderSources(ctx)
	if err != nil {
		return nil, err
	}
	var staged []dbsqlc.ListFBCRenderSourcesRow
	newest := map[string]dbsqlc.ListFBCRenderSourcesRow{}
	for _, r := range rows {
		if r.Nvr == "" {
			staged = append(staged, r)
			continue
		}
		operator, version, ocp, ok := artbuild.ParseFBC(r.Nvr)
		if !ok {
			continue
		}
		// The NVRs of one slot differ only in their build timestamp.
		if k := operator + " " + version + " " + ocp; r.Nvr > newest[k].Nvr {
			newest[k] = r
		}
	}
	byCreated := func(a, b dbsqlc.ListFBCRenderSourcesRow) int {
		return cmp.Or(cmp.Compare(b.Created, a.Created), cmp.Compare(a.Pullspec, b.Pullspec))
	}
	slices.SortFunc(staged, byCreated)
	var pullspecs []string
	for _, r := range append(staged, slices.SortedFunc(maps.Values(newest), byCreated)...) {
		if len(pullspecs) == limit {
			break
		}
		if r.State == fbc.StateRendered || r.State == fbc.StateFailed && parseTime(r.RetryAfter).After(now) || slices.Contains(pullspecs, r.Pullspec) {
			continue
		}
		pullspecs = append(pullspecs, r.Pullspec)
	}
	return pullspecs, nil
}

// UpsertFBCRender stores r unless its image is already rendered.
func (d *DB) UpsertFBCRender(ctx context.Context, r fbc.Render) error {
	var retry string
	if !r.RetryAfter.IsZero() {
		retry = r.RetryAfter.UTC().Format(time.RFC3339)
	}
	return d.queries().UpsertFBCRender(ctx, dbsqlc.UpsertFBCRenderParams{
		Digest:             r.Digest,
		State:              r.State,
		LabelNvr:           r.LabelNVR,
		PackageName:        r.Package,
		Channel:            r.Channel.Name,
		HeadCsv:            r.Channel.HeadCSV,
		HeadBundlePullspec: r.Channel.HeadBundlePullspec,
		Replaces:           r.Channel.Replaces,
		SkipRange:          r.Channel.SkipRange,
		Error:              r.Error,
		RenderedAt:         r.RenderedAt.UTC().Format(time.RFC3339),
		RetryAfter:         retry,
	})
}

func (d *DB) UnjoinedFBCRenders(ctx context.Context) ([]fbc.Render, error) {
	rows, err := d.queries().ListUnjoinedFBCRenders(ctx)
	if err != nil {
		return nil, err
	}
	renders := make([]fbc.Render, len(rows))
	for i, r := range rows {
		renders[i] = fbc.Render{
			Digest:   r.Digest,
			LabelNVR: r.LabelNvr,
			Channel:  fbc.Channel{Name: r.Channel, HeadBundlePullspec: r.HeadBundlePullspec},
			State:    fbc.StateRendered,
		}
	}
	return renders, nil
}
