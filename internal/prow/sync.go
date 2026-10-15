package prow

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

const bucket = "test-platform-results-public"

// DefaultBaseURL is the public, anonymous GCS endpoint.
const DefaultBaseURL = "https://storage.googleapis.com"

// Client reads the public CI results bucket anonymously.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// listDirs returns the names of the directories under dir that start with
// prefix, following page tokens.
func (c *Client) listDirs(ctx context.Context, dir, prefix string) ([]string, error) {
	var names []string
	prefix = dir + prefix
	q := url.Values{"prefix": {prefix}, "delimiter": {"/"}, "fields": {"prefixes,nextPageToken"}}
	for {
		data, err := c.get(ctx, c.baseURL+"/storage/v1/b/"+bucket+"/o?"+q.Encode())
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, fmt.Errorf("list %s: not found", prefix)
		}
		var page struct {
			Prefixes      []string `json:"prefixes"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, p := range page.Prefixes {
			names = append(names, strings.TrimSuffix(strings.TrimPrefix(p, dir), "/"))
		}
		if page.NextPageToken == "" {
			return names, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
}

// object returns the object's body, or nil if it does not exist.
func (c *Client) object(ctx context.Context, name string) ([]byte, error) {
	return c.get(ctx, c.baseURL+"/"+bucket+"/"+name)
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Store is the subset of the database layer the syncer needs.
type Store interface {
	ListFinishedProwBuildIDs(ctx context.Context, jobName string) ([]string, error)
	UpsertProwRun(ctx context.Context, r *Run) error
	ListProwRunStarts(ctx context.Context, jobName string) ([]Run, error)
}

// Syncer polls configured job prefixes and upserts their runs.
type Syncer struct {
	client   *Client
	store    Store
	jobs     []Job
	interval time.Duration
	logger   *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
	// Retention, when set, skips runs older than it that are not stored.
	Retention time.Duration
	// Pattern, when set, adds the jobs it matches to jobs each pass; a job
	// in jobs keeps its application.
	Pattern *JobPattern
}

func NewSyncer(client *Client, store Store, jobs []Job, interval time.Duration, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, store: store, jobs: jobs, interval: interval, logger: logger}
}

// Run syncs immediately and then every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) {
	s.SyncOnce(ctx)
	ticker := time.NewTicker(s.interval)
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

func (s *Syncer) SyncOnce(ctx context.Context) {
	var pass syncstatus.Pass
	jobs, err := s.discover(ctx)
	if err != nil {
		s.logger.Error("discover jobs", "error", err)
		pass.Add(fmt.Errorf("discover jobs: %w", err))
	}
	for _, job := range jobs {
		n, err := s.syncJob(ctx, job)
		if err != nil {
			s.logger.Error("sync job", "job", job.Name, "error", err)
			pass.Add(fmt.Errorf("%s: %w", job.Name, err))
			continue
		}
		s.logger.Info("synced job", "job", job.Name, "fetched", n)
	}
	s.Status.Report(pass.Err())
}

// discover returns the configured jobs and those Pattern matches in the
// bucket. On a listing failure it returns the configured jobs.
func (s *Syncer) discover(ctx context.Context) ([]Job, error) {
	if s.Pattern == nil {
		return s.jobs, nil
	}
	names, err := s.client.listDirs(ctx, "logs/", s.Pattern.prefix)
	if err != nil {
		return s.jobs, err
	}
	jobs := slices.Clone(s.jobs)
	for _, name := range names {
		if slices.ContainsFunc(s.jobs, func(j Job) bool { return j.Name == name }) {
			continue
		}
		if job, ok := s.Pattern.job(name); ok {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// syncJob fetches every run not yet known to be finished. A finished run is
// never fetched again once its artifacts were read. A job with no stored runs
// is read newest first, up to its first run that predates the retention
// window, so a long-idle job does not backfill its history.
func (s *Syncer) syncJob(ctx context.Context, job Job) (int, error) {
	ids, err := s.client.listDirs(ctx, job.Prefix, "")
	if err != nil {
		return 0, err
	}
	finishedIDs, err := s.store.ListFinishedProwBuildIDs(ctx, job.Name)
	if err != nil {
		return 0, err
	}
	done := make(map[string]bool, len(finishedIDs))
	for _, id := range finishedIDs {
		done[id] = true
	}
	expired, unstored, err := s.expired(ctx, job.Name)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-s.Retention)
	if unstored {
		// Build IDs are decimal, so a longer one is larger.
		slices.SortFunc(ids, func(a, b string) int {
			return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(b, a))
		})
	}
	n := 0
	var walked []*Run
	for _, id := range ids {
		if done[id] || expired(id) {
			continue
		}
		run, err := s.fetchRun(ctx, job, id)
		if err != nil {
			return n, fmt.Errorf("run %s: %w", id, err)
		}
		if run == nil {
			continue
		}
		if unstored {
			if run.StartedAt != nil && run.StartedAt.Before(cutoff) {
				break
			}
			walked = append(walked, run)
			continue
		}
		if err := s.store.UpsertProwRun(ctx, run); err != nil {
			return n, fmt.Errorf("upsert run %s: %w", id, err)
		}
		n++
	}
	// The newest stored run floors expired, so the newest-first walk is
	// stored only once complete, and oldest first: a partial store would
	// hide the older in-window runs from every later pass.
	for _, run := range slices.Backward(walked) {
		if err := s.store.UpsertProwRun(ctx, run); err != nil {
			return n, fmt.Errorf("upsert run %s: %w", run.BuildID, err)
		}
		n++
	}
	return n, nil
}

// expired reports whether an unstored run predates the retention window: its
// ID is numerically below those of the job's stored runs started in the
// window and of its newest stored run. Build IDs grow with time; a
// non-numeric one never expires, nor does any while the job has no runs,
// which the second result reports.
func (s *Syncer) expired(ctx context.Context, job string) (func(id string) bool, bool, error) {
	if s.Retention <= 0 {
		return func(string) bool { return false }, false, nil
	}
	runs, err := s.store.ListProwRunStarts(ctx, job)
	if err != nil {
		return nil, false, err
	}
	cutoff := time.Now().Add(-s.Retention)
	stored := make(map[string]bool, len(runs))
	var floor uint64
	// runs is newest first.
	for i, r := range runs {
		stored[r.BuildID] = true
		if i > 0 && (r.StartedAt == nil || r.StartedAt.Before(cutoff)) {
			continue
		}
		if n, err := strconv.ParseUint(r.BuildID, 10, 64); err == nil && (floor == 0 || n < floor) {
			floor = n
		}
	}
	return func(id string) bool {
		n, err := strconv.ParseUint(id, 10, 64)
		return err == nil && n < floor && !stored[id]
	}, len(runs) == 0, nil
}

// fetchRun returns nil when the run has no readable prowjob.json yet.
func (s *Syncer) fetchRun(ctx context.Context, job Job, id string) (*Run, error) {
	dir := job.Prefix + id + "/"
	pjData, err := s.client.object(ctx, dir+"prowjob.json")
	if err != nil || pjData == nil {
		return nil, err
	}
	finData, err := s.client.object(ctx, dir+"finished.json")
	if err != nil {
		return nil, err
	}
	run, target, err := parseRun(job, id, pjData, finData)
	if err != nil {
		s.logger.Warn("skip run", "job", job.Name, "build_id", id, "error", err)
		return nil, nil
	}
	run.FetchedAt = time.Now().UTC()
	if run.CompletedAt == nil {
		return run, nil
	}
	run.ArtifactState = ArtifactNone
	if target == "" {
		return run, nil
	}
	artifacts := dir + "artifacts/" + target + "/quay-gather/artifacts/"
	data, err := s.client.object(ctx, artifacts+"tested-images.json")
	if err != nil {
		return nil, err
	}
	if data != nil {
		if run.applyArtifact(data) && run.State != "success" {
			if run.InstallFailed, err = s.installFailed(ctx, job, id, target); err != nil {
				return nil, err
			}
		}
		return run, nil
	}
	if data, err = s.client.object(ctx, artifacts+"olm-csvs.yaml"); err != nil {
		return nil, err
	}
	if data != nil {
		run.applyOLMCSVs(data)
	}
	return run, nil
}

// installSteps is the Quay install step of each ci-operator target: the
// storage's quay-deploy step creates the quay-operator Subscription, then
// waits for its CSV and the QuayRegistry.
var installSteps = map[string]string{
	"aws-s3-nightly":      "quay-deploy-aws-s3",
	"aws-s3-nightly-fips": "quay-deploy-aws-s3",
	"libvirt-s3-nightly":  "quay-deploy-aws-s3",
	"aws-odf-nightly":     "quay-deploy-odf",
	"azure-blob-nightly":  "quay-deploy-azure-blob",
	"gcp-gcs-nightly":     "quay-deploy-gcp-gcs",
}

// installFailed reports whether the run's Quay install step failed.
func (s *Syncer) installFailed(ctx context.Context, job Job, id, target string) (bool, error) {
	step, ok := installSteps[target]
	if !ok {
		s.logger.Warn("no Quay install step for target", "job", job.Name, "build_id", id, "target", target)
		return false, nil
	}
	data, err := s.client.object(ctx, job.Prefix+id+"/artifacts/"+target+"/"+step+"/finished.json")
	if err != nil || data == nil {
		return false, err
	}
	var fin finished
	return json.Unmarshal(data, &fin) == nil && fin.Result == "FAILURE", nil
}
