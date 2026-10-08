package prow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

// listBuildIDs returns the run directories under prefix, following page tokens.
func (c *Client) listBuildIDs(ctx context.Context, prefix string) ([]string, error) {
	var ids []string
	pageToken := ""
	for {
		q := url.Values{"prefix": {prefix}, "delimiter": {"/"}, "fields": {"prefixes,nextPageToken"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
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
			ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/"))
		}
		if page.NextPageToken == "" {
			return ids, nil
		}
		pageToken = page.NextPageToken
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
	UpsertProwSync(ctx context.Context, s SyncState) error
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
	for _, job := range s.jobs {
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

// syncJob fetches every run not yet known to be finished. A finished run is
// never fetched again.
func (s *Syncer) syncJob(ctx context.Context, job Job) (int, error) {
	ids, err := s.client.listBuildIDs(ctx, job.Prefix)
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
	n := 0
	for _, id := range ids {
		if done[id] {
			continue
		}
		run, err := s.fetchRun(ctx, job, id)
		if err != nil {
			return n, fmt.Errorf("run %s: %w", id, err)
		}
		if run == nil {
			continue
		}
		if err := s.store.UpsertProwRun(ctx, run); err != nil {
			return n, fmt.Errorf("upsert run %s: %w", id, err)
		}
		n++
	}
	return n, s.store.UpsertProwSync(ctx, SyncState{
		JobName:            job.Name,
		Application:        job.Application,
		Interval:           s.interval,
		LastSuccessfulSync: time.Now().UTC(),
	})
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
	if target != "" {
		data, err := s.client.object(ctx, dir+"artifacts/"+target+"/quay-gather/artifacts/tested-images.json")
		if err != nil {
			return nil, err
		}
		if data != nil {
			run.applyArtifact(data)
		}
	}
	return run, nil
}
