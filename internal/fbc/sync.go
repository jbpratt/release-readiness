package fbc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	StateRendered = "rendered"
	StateFailed   = "failed"

	// perPass bounds the images a pass downloads.
	perPass = 20
	// retryAfter spaces out renders of an image that failed.
	retryAfter = time.Hour
)

// Render is the render of an FBC image, by digest. A rendered one is final;
// a failed one, with its Error, is retried after RetryAfter, or never when
// RetryAfter is zero.
type Render struct {
	Digest, LabelNVR       string
	Channel                Channel
	State, Error           string
	RenderedAt, RetryAfter time.Time
}

// Store is the subset of the database layer the render sync needs.
type Store interface {
	// FBCsToRender returns up to limit pullspecs of FBC images to render:
	// none rendered, none failed with no retry or a retry after now.
	FBCsToRender(ctx context.Context, now time.Time, limit int) ([]string, error)
	// UpsertFBCRender stores r unless its image is already rendered.
	UpsertFBCRender(ctx context.Context, r Render) error
	// UnjoinedFBCRenders returns the rendered images whose head bundle has
	// no stored ART bundle build.
	UnjoinedFBCRenders(ctx context.Context) ([]Render, error)
}

// BundleCloser stores the ART bundle build of group whose image has digest,
// as artbuild.HistorySync.CloseBundle does.
type BundleCloser interface {
	CloseBundle(ctx context.Context, group, digest string) error
}

// Sync renders FBC images into the store and joins each head bundle to its
// ART build.
type Sync struct {
	registry *Registry
	store    Store
	closer   BundleCloser
	logger   *slog.Logger
	now      func() time.Time
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewSync(registry *Registry, store Store, closer BundleCloser, logger *slog.Logger) *Sync {
	return &Sync{registry: registry, store: store, closer: closer, logger: logger, now: time.Now}
}

// Run syncs a pass immediately and then every interval until ctx is cancelled.
func (s *Sync) Run(ctx context.Context, interval time.Duration) {
	s.SyncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

// SyncOnce renders up to perPass images, then joins every rendered head
// bundle not yet joined.
func (s *Sync) SyncOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(pass.Err()) }()
	pullspecs, err := s.store.FBCsToRender(ctx, s.now(), perPass)
	if err != nil {
		pass.Add(fmt.Errorf("list FBCs to render: %w", err))
		return
	}
	for _, ps := range pullspecs {
		img, err := s.registry.Read(ctx, ps)
		if ctx.Err() != nil {
			return
		}
		now := s.now()
		r := Render{Digest: ps[strings.Index(ps, "@")+1:], LabelNVR: img.LabelNVR, Channel: img.Channel, State: StateRendered, RenderedAt: now}
		if err != nil {
			r.State, r.Error = StateFailed, err.Error()
			// An image's digest pins its catalog, so a missing channel stays
			// missing.
			if !errors.Is(err, ErrNoChannel) {
				r.RetryAfter = now.Add(retryAfter)
			}
			s.logger.Warn("render", "pullspec", ps, "error", err)
			pass.Add(fmt.Errorf("render %s: %w", ps, err))
		} else {
			s.logger.Info("rendered", "pullspec", ps, "nvr", r.LabelNVR, "channel", r.Channel.Name, "head", r.Channel.HeadCSV)
		}
		if err := s.store.UpsertFBCRender(ctx, r); err != nil {
			pass.Add(fmt.Errorf("store render %s: %w", ps, err))
		}
	}
	unjoined, err := s.store.UnjoinedFBCRenders(ctx)
	if err != nil {
		pass.Add(fmt.Errorf("list unjoined renders: %w", err))
		return
	}
	// Many images of a stream share a head bundle.
	tried := map[string]bool{}
	for _, r := range unjoined {
		// A rendered image's channel is stable-X.Y, so its ART group is quay-X.Y.
		group := "quay-" + strings.TrimPrefix(r.Channel.Name, "stable-")
		digest := r.Channel.HeadBundlePullspec[strings.Index(r.Channel.HeadBundlePullspec, "@")+1:]
		if tried[group+digest] {
			continue
		}
		tried[group+digest] = true
		if err := s.closer.CloseBundle(ctx, group, digest); err != nil {
			if ctx.Err() != nil {
				return
			}
			// The render stays unjoined, so a later pass retries once ART
			// lists the operand.
			if errors.Is(err, artbuild.ErrOperandUnavailable) {
				s.logger.Warn("join head bundle", "digest", digest, "fbc", r.LabelNVR, "error", err)
				continue
			}
			pass.Add(fmt.Errorf("join head bundle %s of %s: %w", digest, r.LabelNVR, err))
		}
	}
}
