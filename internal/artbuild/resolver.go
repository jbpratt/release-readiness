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
	// pendingDays is how far back a stream's search looks for running builds.
	pendingDays = 7
	// searchCap is the most rows /search returns, newest first.
	searchCap = 1000
)

var quayStream = regexp.MustCompile(`quay-(\d+)-(\d+)`)

// Build is the ART build record behind one image digest. Unresolved builds
// carry only Digest, State and CheckedAt.
type Build struct {
	Digest, State, NVR, RecordID string
	UpstreamRepo, UpstreamSHA    string
	CheckedAt                    time.Time
}

// PendingBuild is the newest running ART image build of one NVR name and
// version in a group. Pending builds have no image yet, so they are keyed by
// what they will be, not by digest.
type PendingBuild struct {
	Version, Name string
	NVR, RecordID string
	UpstreamSHA   string
	StartedAt     time.Time
}

// Attempt is one image-build record of a stream search. ART records a
// finished build as another record of its NVR and leaves the pending one.
type Attempt struct {
	Version, Name, NVR string
	RecordID, Outcome  string
	StartedAt          time.Time
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
	// ListActiveApplications returns the Konflux applications of unreleased versions.
	ListActiveApplications(ctx context.Context) ([]string, error)
	// ReplaceArtPendingBuilds replaces group's pending builds with builds.
	ReplaceArtPendingBuilds(ctx context.Context, group string, builds []PendingBuild, checkedAt time.Time) error
	// StoreArtBuildAttempts upserts group's attempts by record id and extends
	// its covered span by [from, to], or restarts it there after a gap.
	StoreArtBuildAttempts(ctx context.Context, group string, attempts []Attempt, from, to time.Time) error
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

// ResolveOnce looks up up to perPass candidates, then refreshes each active
// stream's pending builds. A lookup that fails is left for the next pass; only
// a clean miss is cached as unresolved.
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
	r.refreshPending(ctx, now, pace.C, &pass)
}

// refreshPending searches each active stream once and stores its running
// builds. A failed search keeps the group's last stored set.
func (r *Resolver) refreshPending(ctx context.Context, now time.Time, pace <-chan time.Time, pass *syncstatus.Pass) {
	apps, err := r.store.ListActiveApplications(ctx)
	if err != nil {
		r.logger.Error("list active applications", "error", err)
		pass.Add(fmt.Errorf("list active applications: %w", err))
		return
	}
	var groups []string
	for _, a := range apps {
		if m := quayStream.FindStringSubmatch(a); m != nil {
			if g := "quay-" + m[1] + "." + m[2]; !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
	}
	for _, g := range groups {
		if err := wait(ctx, pace); err != nil {
			return
		}
		from := now.AddDate(0, 0, -pendingDays)
		builds, err := r.client.search(ctx, url.Values{
			"group":     {g},
			"assembly":  {"stream"},
			"dateRange": {from.Format(time.DateOnly) + " to " + now.AddDate(0, 0, 1).Format(time.DateOnly)},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Warn("search pending", "group", g, "error", err)
			pass.Add(fmt.Errorf("search pending %s: %w", g, err))
			continue
		}
		// Stamp the search time, not the pass start: the candidate phase can
		// take minutes and the API hides rows checked too long ago.
		searched := time.Now().UTC()
		if err := r.store.ReplaceArtPendingBuilds(ctx, g, pendingBuilds(builds), searched); err != nil {
			r.logger.Error("store pending", "group", g, "error", err)
			pass.Add(fmt.Errorf("store pending %s: %w", g, err))
		}
		attempts := imageAttempts(builds)
		// A capped answer dropped its oldest rows, so it covers only from
		// the oldest row it kept.
		if len(builds) >= searchCap {
			from = searched
			for _, sb := range builds {
				if t, err := time.Parse(time.RFC1123, sb.StartTime); err == nil && t.Before(from) {
					from = t.UTC()
				}
			}
		}
		if err := r.store.StoreArtBuildAttempts(ctx, g, attempts, from, searched); err != nil {
			r.logger.Error("store attempts", "group", g, "error", err)
			pass.Add(fmt.Errorf("store attempts %s: %w", g, err))
		}
	}
}

// imageAttempts returns the image-build records of a search.
func imageAttempts(builds []searchBuild) []Attempt {
	var out []Attempt
	for _, sb := range builds {
		name, version := SplitNVR(sb.NVR)
		started, err := time.Parse(time.RFC1123, sb.StartTime)
		if sb.Type != "image" || name == "" || sb.RecordID == "" || err != nil {
			continue
		}
		out = append(out, Attempt{
			Version: version, Name: name, NVR: sb.NVR,
			RecordID: sb.RecordID, Outcome: sb.Outcome,
			StartedAt: started.UTC(),
		})
	}
	return out
}

// pendingBuilds keeps, per NVR name and version, the newest image build if it
// is still running, the later record id breaking a start time tie. ART adds a
// finished build's outcome as another row of its NVR and leaves the pending
// row, so a build runs while its NVR has only pending rows.
func pendingBuilds(builds []searchBuild) []PendingBuild {
	finished := map[string]bool{}
	newest := map[[2]string]PendingBuild{}
	for _, sb := range builds {
		if sb.Outcome != "pending" {
			finished[sb.NVR] = true
		}
		if sb.Type != "image" {
			continue
		}
		name, version := SplitNVR(sb.NVR)
		started, err := time.Parse(time.RFC1123, sb.StartTime)
		if name == "" || err != nil {
			continue
		}
		p := PendingBuild{
			Version: version, Name: name,
			NVR: sb.NVR, RecordID: sb.RecordID,
			UpstreamSHA: sb.Commitish,
			StartedAt:   started.UTC(),
		}
		k := [2]string{name, version}
		if cur, ok := newest[k]; !ok || p.StartedAt.After(cur.StartedAt) || p.StartedAt.Equal(cur.StartedAt) && p.RecordID > cur.RecordID {
			newest[k] = p
		}
	}
	var out []PendingBuild
	for _, p := range newest {
		if !finished[p.NVR] {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b PendingBuild) int { return strings.Compare(a.NVR, b.NVR) })
	return out
}

// SplitNVR returns an NVR's name and version, empty when it has no release.
// quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9 is
// quay-quay-container, 3.18.1.
func SplitNVR(nvr string) (name, version string) {
	i := strings.LastIndex(nvr, "-")
	if i < 0 {
		return "", ""
	}
	j := strings.LastIndex(nvr[:i], "-")
	if j <= 0 {
		return "", ""
	}
	return nvr[:j], nvr[j+1 : i]
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
