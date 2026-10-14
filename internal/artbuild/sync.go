package artbuild

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

// Build types of the ART build history.
const (
	TypeFBC    = "fbc"
	TypeBundle = "bundle"
	TypeImage  = "image"
)

const (
	// searchCap is the most rows /search returns; a listing that long may be
	// truncated.
	searchCap = 1000
	// historyDays is how far back the history sync reads.
	historyDays = 120
	// refreshDays are the newest days rescanned every pass, as a build that
	// was pending at a scan may have finished since.
	refreshDays = 3
	// backfillPerPass bounds the older unscanned days a pass reads per group.
	backfillPerPass = 7
	// sourcesPerPass bounds the stored records a pass reads the source repo
	// of, for those stored before it was kept.
	sourcesPerPass = 50
)

// nvrDate is the build date embedded in an image or bundle NVR, as
// 20261008 in quay-clair-container-3.17.6-202610081142.p2... The service
// infers a search date range only from image NVRs.
var nvrDate = regexp.MustCompile(`[.-](\d{8})\d{4}`)

// HistoryBuild is a successful ART build record, with the source commit ART
// built from. StartTime is as ART gives it.
type HistoryBuild struct {
	RecordID, Group, Type, NVR, Digest, Pullspec string
	Commitish, StartTime                         string
}

// HistoryStore is the subset of the database layer the history sync needs.
type HistoryStore interface {
	// ListSnapshotApplications returns every application with a stored Snapshot.
	ListSnapshotApplications(ctx context.Context) ([]string, error)
	// ScannedArtHistoryDays returns the days of group's builds of buildType
	// scanned complete, as YYYY-MM-DD.
	ScannedArtHistoryDays(ctx context.Context, group, buildType string) ([]string, error)
	MarkArtHistoryDayScanned(ctx context.Context, group, buildType, day string, at time.Time) error
	HasArtHistoryRecord(ctx context.Context, recordID string) (bool, error)
	HasArtHistoryNVR(ctx context.Context, group, buildType, nvr string) (bool, error)
	HasArtHistoryDigest(ctx context.Context, buildType, digest string) (bool, error)
	UpsertArtHistoryBuild(ctx context.Context, b HistoryBuild) error
	// UpsertArtSourceRepo stores a record's name and source repo, "" for none.
	UpsertArtSourceRepo(ctx context.Context, recordID, name, sourceRepo string) error
	// ArtHistoryWithoutSource returns up to limit stored records never
	// checked for a source repo; RecordID, NVR and Pullspec are set.
	ArtHistoryWithoutSource(ctx context.Context, limit int) ([]HistoryBuild, error)
}

// HistorySync stores the successful FBC builds of each Quay stream from ART
// build history, day by day, with the bundle each FBC holds and the operator
// and operand images that bundle deploys. A build is stored only once
// everything it names is, so a stored FBC or bundle is closed.
type HistorySync struct {
	client *Client
	store  HistoryStore
	logger *slog.Logger
	now    func() time.Time
	// pace spaces out requests, CloseBundle's included.
	pace *time.Ticker
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewHistorySync(client *Client, store HistoryStore, logger *slog.Logger) *HistorySync {
	return &HistorySync{client: client, store: store, logger: logger, now: time.Now, pace: time.NewTicker(time.Second)}
}

// Run syncs a pass immediately and then every interval until ctx is cancelled.
func (h *HistorySync) Run(ctx context.Context, interval time.Duration) {
	h.SyncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("stopping")
			return
		case <-ticker.C:
			h.SyncOnce(ctx)
		}
	}
}

// SyncOnce scans, for each stream with a stored Snapshot, the newest
// refreshDays and up to backfillPerPass older unscanned days, newest first.
// A day is marked scanned only once all its successful FBC builds are stored.
// It then reads the source repo of up to sourcesPerPass stored records that
// lack one.
func (h *HistorySync) SyncOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { h.Status.Report(pass.Err()) }()
	apps, err := h.store.ListSnapshotApplications(ctx)
	if err != nil {
		pass.Add(fmt.Errorf("list applications: %w", err))
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
	today := h.now().UTC()
	// Each group's days to scan, newest first.
	days := map[string][]string{}
	for _, g := range groups {
		scanned, err := h.store.ScannedArtHistoryDays(ctx, g, TypeFBC)
		if err != nil {
			pass.Add(fmt.Errorf("list scanned days %s: %w", g, err))
			continue
		}
		for i := 0; i < historyDays && len(days[g]) < refreshDays+backfillPerPass; i++ {
			if day := today.AddDate(0, 0, -i).Format(time.DateOnly); i < refreshDays || !slices.Contains(scanned, day) {
				days[g] = append(days[g], day)
			}
		}
	}
	// Take the groups in turn, so a busy group does not hold back the
	// newest days of the others.
	for i := range refreshDays + backfillPerPass {
		for _, g := range groups {
			if i >= len(days[g]) {
				continue
			}
			day := days[g][i]
			if err := h.scanDay(ctx, g, day); err != nil {
				if ctx.Err() != nil {
					return
				}
				h.logger.Warn("scan", "group", g, "day", day, "error", err)
				pass.Add(fmt.Errorf("scan %s %s: %w", g, day, err))
				continue
			}
			if err := h.store.MarkArtHistoryDayScanned(ctx, g, TypeFBC, day, h.now()); err != nil {
				pass.Add(fmt.Errorf("mark %s %s: %w", g, day, err))
			}
		}
	}
	h.backfillSources(ctx, &pass)
}

// backfillSources stores the source repo of up to sourcesPerPass records
// stored without one.
func (h *HistorySync) backfillSources(ctx context.Context, pass *syncstatus.Pass) {
	builds, err := h.store.ArtHistoryWithoutSource(ctx, sourcesPerPass)
	if err != nil {
		pass.Add(fmt.Errorf("list records without source: %w", err))
		return
	}
	for _, b := range builds {
		rec, err := h.read(ctx, searchBuild{NVR: b.NVR, RecordID: b.RecordID, ImagePullspec: b.Pullspec})
		if err == nil {
			err = h.store.UpsertArtSourceRepo(ctx, rec.RecordID, rec.Name, rec.SourceRepo)
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			pass.Add(fmt.Errorf("source of %s: %w", b.NVR, err))
		}
	}
}

// UnscannedDays counts the days before today that the history sync reads and
// scanned, as YYYY-MM-DD, lacks: the backfill still to do. Today is read on
// every pass.
func UnscannedDays(today time.Time, scanned []string) int {
	n := 0
	for i := 1; i < historyDays; i++ {
		if !slices.Contains(scanned, today.UTC().AddDate(0, 0, -i).Format(time.DateOnly)) {
			n++
		}
	}
	return n
}

// scanDay stores the successful FBC builds group started on day.
func (h *HistorySync) scanDay(ctx context.Context, group, day string) error {
	q := url.Values{"group": {group}, "buildtype": {TypeFBC}, "name": {"fbc$"}, "dateRange": {day + " to " + day}}
	rows, err := h.searchWindow(ctx, q)
	if err != nil {
		return err
	}
	stored := 0
	for _, sb := range rows {
		if sb.Outcome != "success" || sb.ImagePullspec == "" {
			continue
		}
		ok, err := h.store.HasArtHistoryRecord(ctx, sb.RecordID)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		rec, err := h.read(ctx, sb)
		if err != nil {
			return err
		}
		for _, n := range rec.BundleNVRs {
			if err := h.closeBundleNVR(ctx, group, n); err != nil {
				return fmt.Errorf("%s: %w", rec.NVR, err)
			}
		}
		if err := h.upsert(ctx, group, TypeFBC, rec); err != nil {
			return err
		}
		stored++
	}
	if stored > 0 {
		h.logger.Info("stored FBC builds", "group", group, "day", day, "stored", stored)
	}
	return nil
}

// Operators are the operators a Quay release always ships an FBC of, each
// built as <operator>-fbc.
var Operators = []string{"quay-operator", "container-security-operator", "quay-bridge-operator"}

// searchWindow returns every row of q. A listing at the cap is split by
// build name, the operators' FBC names and any other the truncated listing
// shows, as a name past the cut shows nowhere; a name that alone reaches the
// cap cannot be split, and any warning means the listing may not be what was
// asked, so both are errors.
func (h *HistorySync) searchWindow(ctx context.Context, q url.Values) ([]searchBuild, error) {
	rows, err := h.search(ctx, q)
	if err != nil || len(rows) < searchCap {
		return rows, err
	}
	var names []string
	for _, o := range Operators {
		names = append(names, o+"-fbc")
	}
	for _, sb := range rows {
		if !slices.Contains(names, sb.Name) {
			names = append(names, sb.Name)
		}
	}
	var all []searchBuild
	for _, n := range names {
		sq := maps.Clone(q)
		sq.Set("name", "^"+regexp.QuoteMeta(n)+"$")
		sub, err := h.search(ctx, sq)
		if err != nil {
			return nil, err
		}
		if len(sub) >= searchCap {
			return nil, fmt.Errorf("%d rows of %s reach the search cap of %d", len(sub), n, searchCap)
		}
		all = append(all, sub...)
	}
	return all, nil
}

func (h *HistorySync) search(ctx context.Context, q url.Values) ([]searchBuild, error) {
	if err := wait(ctx, h.pace.C); err != nil {
		return nil, err
	}
	rows, warnings, err := h.client.search(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		return nil, fmt.Errorf("search warnings: %s", strings.Join(warnings, "; "))
	}
	return rows, nil
}

// read returns the full record of a successful row.
func (h *HistorySync) read(ctx context.Context, sb searchBuild) (*record, error) {
	if err := wait(ctx, h.pace.C); err != nil {
		return nil, err
	}
	rec, err := h.client.record(ctx, sb.NVR, sb.RecordID)
	if err != nil {
		return nil, err
	}
	if rec.Outcome != "success" || rec.ImagePullspec != sb.ImagePullspec {
		return nil, fmt.Errorf("record %s %s is %s of %q, not a success of %q", sb.NVR, sb.RecordID, rec.Outcome, rec.ImagePullspec, sb.ImagePullspec)
	}
	return rec, nil
}

// CloseBundle stores the successful bundle build of group whose image has
// digest, with the operator and operand images it deploys, unless stored.
func (h *HistorySync) CloseBundle(ctx context.Context, group, digest string) error {
	if ok, err := h.store.HasArtHistoryDigest(ctx, TypeBundle, digest); err != nil || ok {
		return err
	}
	today := h.now().UTC()
	q := url.Values{
		"group":         {group},
		"buildtype":     {TypeBundle},
		"image_sha_tag": {strings.TrimPrefix(digest, "sha256:")},
		"dateRange":     {today.AddDate(0, 0, -historyDays).Format(time.DateOnly) + " to " + today.Format(time.DateOnly)},
	}
	return h.closeBundle(ctx, group, q, func(sb searchBuild) bool { return strings.HasSuffix(sb.ImagePullspec, "@"+digest) })
}

// closeBundleNVR stores the successful bundle build nvr of group, with the
// operator and operand images it deploys, unless stored.
func (h *HistorySync) closeBundleNVR(ctx context.Context, group, nvr string) error {
	if ok, err := h.store.HasArtHistoryNVR(ctx, group, TypeBundle, nvr); err != nil || ok {
		return err
	}
	q, err := nvrQuery(group, nvr)
	if err != nil {
		return err
	}
	q.Set("buildtype", TypeBundle)
	return h.closeBundle(ctx, group, q, func(sb searchBuild) bool { return sb.NVR == nvr })
}

func (h *HistorySync) closeBundle(ctx context.Context, group string, q url.Values, match func(searchBuild) bool) error {
	sb, err := h.findSuccess(ctx, q, match)
	if err != nil {
		return err
	}
	rec, err := h.read(ctx, *sb)
	if err != nil {
		return err
	}
	for _, n := range append([]string{rec.OperatorNVR}, rec.OperandNVRs...) {
		// "external" stands for operands ART does not build.
		if n == "" || n == "external" {
			continue
		}
		if err := h.storeImage(ctx, group, n); err != nil {
			return fmt.Errorf("%s: %w", rec.NVR, err)
		}
	}
	return h.upsert(ctx, group, TypeBundle, rec)
}

// storeImage stores the successful image build nvr of group unless stored.
func (h *HistorySync) storeImage(ctx context.Context, group, nvr string) error {
	if ok, err := h.store.HasArtHistoryNVR(ctx, group, TypeImage, nvr); err != nil || ok {
		return err
	}
	q, err := nvrQuery(group, nvr)
	if err != nil {
		return err
	}
	sb, err := h.findSuccess(ctx, q, func(sb searchBuild) bool { return sb.NVR == nvr })
	if err != nil {
		return err
	}
	rec, err := h.read(ctx, *sb)
	if err != nil {
		return err
	}
	return h.upsert(ctx, group, TypeImage, rec)
}

// findSuccess returns the successful row of q that match accepts; pending
// rows of the same build have no image and never match.
func (h *HistorySync) findSuccess(ctx context.Context, q url.Values, match func(searchBuild) bool) (*searchBuild, error) {
	rows, err := h.search(ctx, q)
	if err != nil {
		return nil, err
	}
	for i, sb := range rows {
		if sb.Outcome == "success" && sb.ImagePullspec != "" && match(sb) {
			return &rows[i], nil
		}
	}
	return nil, fmt.Errorf("no successful build for %s", q.Encode())
}

// nvrQuery searches group for nvr over the days around its embedded build
// date.
func nvrQuery(group, nvr string) (url.Values, error) {
	m := nvrDate.FindStringSubmatch(nvr)
	if m == nil {
		return nil, fmt.Errorf("no build date in %s", nvr)
	}
	d, err := time.Parse("20060102", m[1])
	if err != nil {
		return nil, fmt.Errorf("build date of %s: %w", nvr, err)
	}
	return url.Values{
		"group":     {group},
		"nvr":       {nvr},
		"dateRange": {d.AddDate(0, 0, -1).Format(time.DateOnly) + " to " + d.AddDate(0, 0, 3).Format(time.DateOnly)},
	}, nil
}

// upsert stores rec and its source repo. A record stored without its source
// repo gets it from backfillSources.
func (h *HistorySync) upsert(ctx context.Context, group, buildType string, rec *record) error {
	if err := h.store.UpsertArtHistoryBuild(ctx, historyBuild(group, buildType, rec)); err != nil {
		return err
	}
	return h.store.UpsertArtSourceRepo(ctx, rec.RecordID, rec.Name, rec.SourceRepo)
}

func historyBuild(group, buildType string, rec *record) HistoryBuild {
	return HistoryBuild{
		RecordID:  rec.RecordID,
		Group:     group,
		Type:      buildType,
		NVR:       rec.NVR,
		Digest:    rec.ImagePullspec[strings.Index(rec.ImagePullspec, "@")+1:],
		Pullspec:  rec.ImagePullspec,
		Commitish: rec.Commitish,
		StartTime: rec.StartTime,
	}
}
