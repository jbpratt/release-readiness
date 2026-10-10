package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

const (
	StatePending    = "pending"
	StateScanned    = "scanned"
	StateScanFailed = "scan_failed"
	StateNotScanned = "not_scanned"

	// BasisArchFindings counts a CVE once per architecture it is found in:
	// the scan tasks report totals, not CVE IDs.
	BasisArchFindings = "scanner_arch_findings"

	// retryAfter spaces out reads of a pending scan.
	retryAfter = 10 * time.Minute
	perPass    = 20
)

// pipelineRunPage is a Konflux UI PipelineRun page, as ART links builds:
// .../ns/<namespace>/applications/<application>/pipelineruns/<name>.
var pipelineRunPage = regexp.MustCompile(`/ns/([^/]+)/(?:.+/)?pipelineruns/([^/?#]+)`)

// Scan is the stored scan of one image digest. Counts and Reports are set
// only when scanned; Reports maps each architecture manifest digest to the
// digest of its report. DetailURL is the build PipelineRun's Konflux UI page
// from its ART record, and PipelineRun its name once the page parses.
type Scan struct {
	Digest, State          string
	PipelineRun, DetailURL string
	Counts                 *model.ScanCounts
	Reports                map[string]string
	CheckedAt              time.Time
}

// Store is the subset of the database layer the sync needs.
type Store interface {
	ResolvedArtBuilds(ctx context.Context, digests []string) (map[string]artbuild.Build, error)
	// ImageScans returns the stored scans among digests, without Reports.
	ImageScans(ctx context.Context, digests []string) (map[string]Scan, error)
	UpsertImageScan(ctx context.Context, s Scan) error
}

// Syncer reads the scans of the images images returns, finding each image's
// build PipelineRun through its ART record.
type Syncer struct {
	results *Client
	art     *artbuild.Client
	store   Store
	images  func(context.Context) ([]string, error)
	logger  *slog.Logger
	gap     time.Duration
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewSyncer(results *Client, art *artbuild.Client, store Store, images func(context.Context) ([]string, error), logger *slog.Logger) *Syncer {
	return &Syncer{results: results, art: art, store: store, images: images, logger: logger, gap: time.Second}
}

// Run syncs a pass immediately and then every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
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

// SyncOnce reads up to perPass scans, of images with none stored first, then
// of pending ones read before retryAfter. An image waits for ART to resolve
// it. A read that fails writes nothing and is retried next pass.
func (s *Syncer) SyncOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(pass.Err()) }()
	now := time.Now().UTC()
	digests, err := s.images(ctx)
	if err != nil {
		s.logger.Error("list images", "error", err)
		pass.Add(fmt.Errorf("list images: %w", err))
		return
	}
	builds, err := s.store.ResolvedArtBuilds(ctx, digests)
	if err != nil {
		s.logger.Error("list art builds", "error", err)
		pass.Add(fmt.Errorf("list art builds: %w", err))
		return
	}
	scans, err := s.store.ImageScans(ctx, digests)
	if err != nil {
		s.logger.Error("list scans", "error", err)
		pass.Add(fmt.Errorf("list scans: %w", err))
		return
	}
	var unread, retry []string
	for _, d := range digests {
		if _, ok := builds[d]; !ok {
			continue
		}
		if sc, ok := scans[d]; !ok {
			unread = append(unread, d)
		} else if sc.State == StatePending && sc.CheckedAt.Before(now.Add(-retryAfter)) {
			retry = append(retry, d)
		}
	}
	due := append(unread, retry...)
	due = due[:min(len(due), perPass)]
	pace := time.NewTicker(s.gap)
	defer pace.Stop()
	scanned := 0
	for _, d := range due {
		sc, err := s.read(ctx, pace.C, builds[d], scans[d])
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Warn("read scan", "digest", d, "error", err)
			pass.Add(fmt.Errorf("read scan %s: %w", d, err))
			continue
		}
		sc.CheckedAt = now
		if err := s.store.UpsertImageScan(ctx, sc); err != nil {
			s.logger.Error("store scan", "digest", d, "error", err)
			pass.Add(fmt.Errorf("store scan %s: %w", d, err))
			continue
		}
		if sc.State == StateScanned {
			scanned++
		}
	}
	if len(due) > 0 {
		s.logger.Info("read scans", "read", len(due), "scanned", scanned)
	}
}

// read reads the scan of the image ART built as b, stored as prev. It
// returns an error only when a service could not be read.
func (s *Syncer) read(ctx context.Context, pace <-chan time.Time, b artbuild.Build, prev Scan) (Scan, error) {
	sc := Scan{Digest: b.Digest, State: StatePending, PipelineRun: prev.PipelineRun, DetailURL: prev.DetailURL}
	if sc.PipelineRun == "" {
		if err := wait(ctx, pace); err != nil {
			return sc, err
		}
		var err error
		if sc.DetailURL, err = s.art.BuildPipelineURL(ctx, b.NVR, b.RecordID); err != nil {
			return sc, fmt.Errorf("read ART record: %w", err)
		}
	}
	m := pipelineRunPage.FindStringSubmatch(sc.DetailURL)
	if m == nil {
		return sc, nil
	}
	ns, plr := m[1], m[2]
	sc.PipelineRun = plr
	plrs, err := s.results.records(ctx, pace, ns, fmt.Sprintf(`data_type == "tekton.dev/v1.PipelineRun" && data.metadata.name == %q`, plr))
	if err != nil {
		return sc, fmt.Errorf("read PipelineRun %s: %w", plr, err)
	}
	if len(plrs) == 0 || !plrs[0].done() {
		return sc, nil
	}
	const task = "roxctl-scan"
	n := plrs[0].children(task)
	if n == 0 {
		sc.State = StateNotScanned
		return sc, nil
	}
	trs, err := s.results.records(ctx, pace, ns, fmt.Sprintf(`data_type == "tekton.dev/v1.TaskRun" && data.metadata.labels["tekton.dev/pipelineRun"] == %q && data.metadata.labels["tekton.dev/pipelineTask"] == %q`, plr, task))
	if err != nil {
		return sc, fmt.Errorf("read %s TaskRuns of %s: %w", task, plr, err)
	}
	// Results has not stored every TaskRun yet.
	if len(trs) < n {
		return sc, nil
	}
	slices.SortStableFunc(trs, func(a, b run) int { return b.Status.CompletionTime.Compare(a.Status.CompletionTime) })
	roxctl(&sc, trs)
	return sc, nil
}

// roxctl takes the newest TaskRun that scanned, trs newest first. Each matrix
// TaskRun reports on every architecture, so their counts are never summed.
func roxctl(sc *Scan, trs []run) {
	for _, tr := range trs {
		if counts, reports, ok := tr.scanned(); ok {
			sc.State, sc.Counts, sc.Reports = StateScanned, counts, reports
			return
		}
	}
	if !slices.ContainsFunc(trs, func(tr run) bool { return !tr.done() }) {
		sc.State = StateScanFailed
	}
}

// scanned returns the counts and reports of a scan TaskRun whose TEST_OUTPUT
// is SUCCESS, whose SCAN_OUTPUT parses and whose REPORTS names a report.
// SCAN_OUTPUT's vulnerabilities have a fix and its unpatched ones do not.
func (r *run) scanned() (*model.ScanCounts, map[string]string, bool) {
	var test struct {
		Result string `json:"result"`
	}
	var out struct {
		Fixable *model.SeverityCounts `json:"vulnerabilities"`
		NoFix   *model.SeverityCounts `json:"unpatched_vulnerabilities"`
	}
	var reports map[string]string
	if json.Unmarshal([]byte(r.result("TEST_OUTPUT")), &test) != nil || test.Result != "SUCCESS" ||
		json.Unmarshal([]byte(r.result("SCAN_OUTPUT")), &out) != nil || out.Fixable == nil || out.NoFix == nil ||
		json.Unmarshal([]byte(r.result("REPORTS")), &reports) != nil || len(reports) == 0 {
		return nil, nil, false
	}
	return &model.ScanCounts{Basis: BasisArchFindings, Fixable: *out.Fixable, NoFix: *out.NoFix}, reports, true
}
