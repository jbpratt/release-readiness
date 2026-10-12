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
	// fbcPerPass bounds the FBC records a pass reads for FBC images resolved
	// before their records were kept.
	fbcPerPass = 500
)

var quayStream = regexp.MustCompile(`quay-(\d+)-(\d+)`)

// fbcNVR is an FBC image's NVR, as quay-operator-fbc-3.18.1-20261009044841.ocp4.21.
var fbcNVR = regexp.MustCompile(`^(.+)-fbc-(\d+\.\d+\.\d+)-\d+\.ocp(\d+\.\d+)$`)

// ParseFBC returns the operator, X.Y.Z version and OCP version of an FBC
// image's NVR, and false for any other NVR.
func ParseFBC(nvr string) (operator, version, ocp string, ok bool) {
	m := fbcNVR.FindStringSubmatch(nvr)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// Build is the ART build record behind one image digest. Unresolved builds
// carry only Digest, State and CheckedAt.
type Build struct {
	Digest, State, NVR, RecordID string
	CheckedAt                    time.Time
}

// FBCRecord is what the ART build record of an FBC image says of it:
// BundleNVRs, the operator bundles it holds, is empty unless the build
// succeeded, and EndTime is as ART gives it.
type FBCRecord struct {
	Digest, Pullspec, EndTime string
	BundleNVRs                []string
}

// FBCImage is a resolved FBC image, an operator's catalog for an OCP version,
// with its FBC record, nil until read.
type FBCImage struct {
	NVR, Operator, OCP string
	Record             *FBCRecord
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
	// ListUnreadFBCImages returns up to limit resolved FBC images without an
	// FBC record.
	ListUnreadFBCImages(ctx context.Context, limit int) ([]Build, error)
	UpsertFBCRecord(ctx context.Context, r FBCRecord) error
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

// ResolveOnce looks up up to perPass candidates, then reads the FBC records of
// up to fbcPerPass FBC images resolved without one. A lookup or read that
// fails is left for the next pass; only a clean miss is cached as unresolved.
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
		b, fbc, err := r.resolve(ctx, c, now, pace.C)
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
		if fbc != nil {
			if err := r.store.UpsertFBCRecord(ctx, *fbc); err != nil {
				r.logger.Error("store FBC record", "image", c.Image, "error", err)
				pass.Add(fmt.Errorf("store FBC record %s: %w", c.Image, err))
			}
		}
		if b.State == StateResolved {
			resolved++
		}
	}
	if len(cands) > 0 {
		r.logger.Info("resolved images", "looked_up", len(cands), "resolved", resolved)
	}
	r.readFBCRecords(ctx, &pass, pace.C)
}

// readFBCRecords reads the ART build records of up to fbcPerPass resolved FBC
// images without an FBC record, one request each.
func (r *Resolver) readFBCRecords(ctx context.Context, pass *syncstatus.Pass, pace <-chan time.Time) {
	unread, err := r.store.ListUnreadFBCImages(ctx, fbcPerPass)
	if err != nil {
		r.logger.Error("list unread FBC images", "error", err)
		pass.Add(fmt.Errorf("list unread FBC images: %w", err))
		return
	}
	read := 0
	for _, b := range unread {
		if err := wait(ctx, pace); err != nil {
			return
		}
		rec, err := r.client.record(ctx, b.NVR, b.RecordID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Warn("read FBC record", "nvr", b.NVR, "error", err)
			pass.Add(fmt.Errorf("read FBC record %s: %w", b.NVR, err))
			continue
		}
		if err := r.store.UpsertFBCRecord(ctx, fbcRecord(b.Digest, rec)); err != nil {
			r.logger.Error("store FBC record", "nvr", b.NVR, "error", err)
			pass.Add(fmt.Errorf("store FBC record %s: %w", b.NVR, err))
			continue
		}
		read++
	}
	if len(unread) > 0 {
		r.logger.Info("read FBC records", "unread", len(unread), "read", read)
	}
}

// resolve looks c up, and returns its build and, for an FBC image, its FBC
// record from the same /build read.
func (r *Resolver) resolve(ctx context.Context, c Candidate, now time.Time, pace <-chan time.Time) (Build, *FBCRecord, error) {
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
			return b, nil, err
		}
		builds, _, err := r.client.search(ctx, q)
		if err != nil {
			return b, nil, err
		}
		// image_sha_tag also matches tags and NVRs, and pending rows have no
		// image, so only an exact pullspec is the build.
		if i := slices.IndexFunc(builds, func(sb searchBuild) bool { return sb.ImagePullspec == c.Image }); i >= 0 {
			hit = &builds[i]
			break
		}
	}
	if hit == nil {
		return b, nil, nil
	}
	if err := wait(ctx, pace); err != nil {
		return b, nil, err
	}
	rec, err := r.client.record(ctx, hit.NVR, hit.RecordID)
	if err != nil {
		return b, nil, err
	}
	if rec.ImagePullspec != c.Image {
		return b, nil, nil
	}
	b.State = StateResolved
	b.NVR, b.RecordID = rec.NVR, rec.RecordID
	if _, _, _, ok := ParseFBC(rec.NVR); !ok {
		return b, nil, nil
	}
	fbc := fbcRecord(b.Digest, rec)
	return b, &fbc, nil
}

// fbcRecord is the FBC record of the image of digest by its ART build record.
func fbcRecord(digest string, rec *record) FBCRecord {
	bundles := []string{}
	// A failed build's catalog holds nothing to release.
	if rec.Outcome == "success" {
		bundles = append(bundles, rec.BundleNVRs...)
	}
	return FBCRecord{Digest: digest, Pullspec: rec.ImagePullspec, EndTime: rec.EndTime, BundleNVRs: bundles}
}

func wait(ctx context.Context, pace <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pace:
		return nil
	}
}
