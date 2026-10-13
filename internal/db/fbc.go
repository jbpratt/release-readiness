package db

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/fbc"
)

// fbcRenderSources returns the FBC images the render sync reads, each once:
// the catalogs of staged FBC Snapshots, then the newest ART FBC build of
// each operator, version and OCP, each newest first.
func (d *DB) fbcRenderSources(ctx context.Context) ([]dbsqlc.ListFBCRenderSourcesRow, error) {
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
	var sources []dbsqlc.ListFBCRenderSourcesRow
	seen := map[string]bool{}
	for _, r := range append(staged, slices.SortedFunc(maps.Values(newest), byCreated)...) {
		if !seen[r.Pullspec] {
			seen[r.Pullspec] = true
			sources = append(sources, r)
		}
	}
	return sources, nil
}

// FBCsToRender returns up to limit pullspecs of FBC images to render, in the
// order of fbcRenderSources. It skips rendered images and failed ones to
// retry after now.
func (d *DB) FBCsToRender(ctx context.Context, now time.Time, limit int) ([]string, error) {
	sources, err := d.fbcRenderSources(ctx)
	if err != nil {
		return nil, err
	}
	var pullspecs []string
	for _, r := range sources {
		if len(pullspecs) == limit {
			break
		}
		if r.State == fbc.StateRendered || r.State == fbc.StateFailed && parseTime(r.RetryAfter).After(now) {
			continue
		}
		pullspecs = append(pullspecs, r.Pullspec)
	}
	return pullspecs, nil
}

// FBCReadProgress counts the FBC images of digests the render sync reads by
// the state of their render: rendered, failed, or none yet.
func (d *DB) FBCReadProgress(ctx context.Context, digests []string) (read, failed, pending int, err error) {
	sources, err := d.fbcRenderSources(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	want := map[string]bool{}
	for _, dg := range digests {
		want[dg] = true
	}
	for _, r := range sources {
		_, digest, _ := strings.Cut(r.Pullspec, "@")
		if !want[digest] {
			continue
		}
		// Count a digest once, whichever repo names it.
		delete(want, digest)
		switch r.State {
		case fbc.StateRendered:
			read++
		case fbc.StateFailed:
			failed++
		default:
			pending++
		}
	}
	return read, failed, pending, nil
}

// UpsertFBCRender stores r, with its related images, unless its image is
// already rendered.
func (d *DB) UpsertFBCRender(ctx context.Context, r fbc.Render) error {
	var retry string
	if !r.RetryAfter.IsZero() {
		retry = r.RetryAfter.UTC().Format(time.RFC3339)
	}
	ch := r.Channel
	return d.InTx(ctx, func(tx *DB) error {
		q := tx.queries()
		if err := q.UpsertFBCRender(ctx, dbsqlc.UpsertFBCRenderParams{
			Digest:             r.Digest,
			State:              r.State,
			LabelNvr:           r.LabelNVR,
			Channel:            ch.Name,
			HeadCsv:            ch.HeadCSV,
			HeadBundlePullspec: ch.HeadBundlePullspec,
			PreviousVersion:    ch.Previous.Version,
			Error:              r.Error,
			RenderedAt:         r.RenderedAt.UTC().Format(time.RFC3339),
			RetryAfter:         retry,
		}); err != nil {
			return err
		}
		for role, images := range map[string][]fbc.RelatedImage{"head": ch.HeadRelatedImages, "previous": ch.Previous.RelatedImages} {
			for _, img := range images {
				if err := q.InsertFBCRelatedImage(ctx, dbsqlc.InsertFBCRelatedImageParams{Digest: r.Digest, Role: role, Name: img.Name, Image: img.Image}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// FBCRelatedImages returns the related images of the head bundle and of the
// previous entry's bundle of the FBC image with digest.
func (d *DB) FBCRelatedImages(ctx context.Context, digest string) (head, previous []fbc.RelatedImage, err error) {
	rows, err := d.queries().ListFBCRelatedImages(ctx, digest)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		img := fbc.RelatedImage{Name: r.Name, Image: r.Image}
		if r.Role == "head" {
			head = append(head, img)
		} else {
			previous = append(previous, img)
		}
	}
	return head, previous, nil
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
