// Package scan keeps the CVE scan of each candidate image, read from the scan
// TaskRuns of the PipelineRun that built it in Tekton Results.
package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pageSize is the records asked for per page; the API rejects fewer than 6.
const pageSize = "50"

// Client reads records from the Tekton Results API.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// run is the part of a PipelineRun or TaskRun record the sync reads.
type run struct {
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		CompletionTime  time.Time `json:"completionTime"`
		ChildReferences []struct {
			PipelineTaskName string `json:"pipelineTaskName"`
		} `json:"childReferences"`
		Results []struct {
			Name string `json:"name"`
			// Value is any JSON value; the scan tasks' results are strings.
			Value json.RawMessage `json:"value"`
		} `json:"results"`
	} `json:"status"`
}

// done reports whether the run finished: its Succeeded condition is True or False.
func (r *run) done() bool {
	for _, c := range r.Status.Conditions {
		if c.Type == "Succeeded" {
			return c.Status == "True" || c.Status == "False"
		}
	}
	return false
}

// children counts the run's child TaskRuns of pipeline task name.
func (r *run) children(name string) int {
	n := 0
	for _, c := range r.Status.ChildReferences {
		if c.PipelineTaskName == name {
			n++
		}
	}
	return n
}

// result returns the run's string result name, "" when it has none.
func (r *run) result(name string) string {
	for _, res := range r.Status.Results {
		var s string
		if res.Name == name && json.Unmarshal(res.Value, &s) == nil {
			return s
		}
	}
	return ""
}

// records returns the runs of every record in namespace ns that the CEL
// filter matches, waiting on pace before each page.
func (c *Client) records(ctx context.Context, pace <-chan time.Time, ns, filter string) ([]run, error) {
	q := url.Values{"filter": {filter}, "page_size": {pageSize}}
	var runs []run
	for {
		if err := wait(ctx, pace); err != nil {
			return nil, err
		}
		var page struct {
			Records []struct {
				Data struct {
					// Value is the run's JSON, base64 encoded.
					Value []byte `json:"value"`
				} `json:"data"`
			} `json:"records"`
			NextPageToken string `json:"next_page_token"`
		}
		if err := c.get(ctx, "/apis/results.tekton.dev/v1alpha2/parents/"+url.PathEscape(ns)+"/results/-/records?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		for _, rec := range page.Records {
			var r run
			if err := json.Unmarshal(rec.Data.Value, &r); err != nil {
				return nil, fmt.Errorf("decode record: %w", err)
			}
			runs = append(runs, r)
		}
		if page.NextPageToken == "" {
			return runs, nil
		}
		q.Set("page_token", page.NextPageToken)
	}
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list records: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func wait(ctx context.Context, pace <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pace:
		return nil
	}
}
