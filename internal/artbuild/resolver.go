package artbuild

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	StateResolved   = "resolved"
	StateUnresolved = "unresolved"

	// retryAfter spaces out lookups of a digest the service had no record for.
	retryAfter = 24 * time.Hour
	// searchWindow is how long before its first Snapshot an image may have been built.
	searchWindow = 120 * 24 * time.Hour
	perPass      = 50
)

var quayStream = regexp.MustCompile(`quay-(\d+)-(\d+)`)

// Build is the ART build record behind one image digest. Unresolved builds
// carry only Digest, State and CheckedAt.
type Build struct {
	Digest, State, NVR, RecordID string
	UpstreamRepo, UpstreamSHA    string
	CheckedAt                    time.Time
}

// Candidate is a stored component image to look up. Applications holds every
// application with a stored Snapshot holding it; FirstSeen is the oldest one.
type Candidate struct {
	Image, Component string
	Applications     []string
	FirstSeen        time.Time
}

// Store is the subset of the database layer the resolver needs.
type Store interface {
	ListArtBuildCandidates(ctx context.Context, retryBefore time.Time, limit int) ([]Candidate, error)
	UpsertArtBuild(ctx context.Context, b Build) error
}

// Resolver looks up stored component images in ART build history, one
// request per gap, and caches what it finds.
type Resolver struct {
	client *Client
	store  Store
	logger *slog.Logger
	gap    time.Duration
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewResolver(client *Client, store Store, logger *slog.Logger) *Resolver {
	return &Resolver{client: client, store: store, logger: logger, gap: time.Second}
}

// Run resolves a pass immediately and then every interval until ctx is cancelled.
func (r *Resolver) Run(ctx context.Context, interval time.Duration) {
	r.ResolveOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("stopping")
			return
		case <-ticker.C:
			r.ResolveOnce(ctx)
		}
	}
}

// ResolveOnce looks up up to perPass candidates. A lookup that fails is left
// for the next pass; only a clean miss is cached as unresolved.
func (r *Resolver) ResolveOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { r.Status.Report(pass.Err()) }()
	now := time.Now().UTC()
	cands, err := r.store.ListArtBuildCandidates(ctx, now.Add(-retryAfter), perPass)
	if err != nil {
		r.logger.Error("list candidates", "error", err)
		pass.Add(fmt.Errorf("list candidates: %w", err))
		return
	}
	pace := time.NewTicker(r.gap)
	defer pace.Stop()
	resolved := 0
	for _, c := range cands {
		b, err := r.resolve(ctx, c, now, pace.C)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Warn("lookup", "image", c.Image, "error", err)
			pass.Add(fmt.Errorf("lookup %s: %w", c.Image, err))
			continue
		}
		if err := r.store.UpsertArtBuild(ctx, b); err != nil {
			r.logger.Error("store", "image", c.Image, "error", err)
			pass.Add(fmt.Errorf("store %s: %w", c.Image, err))
			continue
		}
		if b.State == StateResolved {
			resolved++
		}
	}
	if len(cands) > 0 {
		r.logger.Info("resolved images", "looked_up", len(cands), "resolved", resolved)
	}
}

func (r *Resolver) resolve(ctx context.Context, c Candidate, now time.Time, pace <-chan time.Time) (Build, error) {
	b := Build{Digest: c.Image[strings.Index(c.Image, "@")+1:], State: StateUnresolved, CheckedAt: now}
	q := url.Values{
		"image_sha_tag": {strings.TrimPrefix(b.Digest, "sha256:")},
		"assembly":      {"stream"},
		"dateRange":     {c.FirstSeen.Add(-searchWindow).Format(time.DateOnly) + " to " + c.FirstSeen.Add(24*time.Hour).Format(time.DateOnly)},
	}
	// Without a group the service searches only its default group. Stage
	// Snapshots drop the stream from component names and quay-images-base
	// has it only there, so try the applications first. An image held by
	// two streams was built in one of them, so a miss tries the next group.
	var groups []string
	for _, s := range append(slices.Clone(c.Applications), c.Component) {
		if m := quayStream.FindStringSubmatch(s); m != nil {
			if g := "quay-" + m[1] + "." + m[2]; !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
	}
	if len(groups) == 0 {
		groups = []string{""}
	}
	var hit *searchBuild
	for _, g := range groups {
		if g != "" {
			q.Set("group", g)
		}
		if err := wait(ctx, pace); err != nil {
			return b, err
		}
		builds, err := r.client.search(ctx, q)
		if err != nil {
			return b, err
		}
		// image_sha_tag also matches tags and NVRs, and pending rows have no
		// image, so only an exact pullspec is the build.
		if i := slices.IndexFunc(builds, func(sb searchBuild) bool { return sb.ImagePullspec == c.Image }); i >= 0 {
			hit = &builds[i]
			break
		}
	}
	if hit == nil {
		return b, nil
	}
	if err := wait(ctx, pace); err != nil {
		return b, err
	}
	rec, err := r.client.record(ctx, hit.NVR, hit.RecordID)
	if err != nil {
		return b, err
	}
	if rec.ImagePullspec != c.Image {
		return b, nil
	}
	b.State = StateResolved
	b.NVR, b.RecordID = rec.NVR, rec.RecordID
	b.UpstreamRepo, b.UpstreamSHA = rec.SourceRepo, rec.Commitish
	return b, nil
}

func wait(ctx context.Context, pace <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pace:
		return nil
	}
}
