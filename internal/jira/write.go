package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// TargetVersionField is the PROJQUAY Target Version custom field.
const TargetVersionField = "customfield_10855"

// WriteIssue is an issue's live state, as the FBC writer needs it.
type WriteIssue struct {
	Key, Project, Type, Status string
	TargetVersions             []string
}

// EditMeta says which of the fields the writer edits are editable.
type EditMeta struct {
	TargetVersion bool
}

// Transition is a workflow transition offered on an issue.
type Transition struct {
	ID, Name string
	// To is the status the transition moves the issue to.
	To string
}

type Comment struct {
	ID, Body string
}

// WriteClient reads and writes issues with its own credential. It uses the
// v2 API, whose comment bodies are plain text rather than ADF.
type WriteClient struct {
	c  *Client
	mu sync.Mutex
	// unit is one second of Retry-After or backoff.
	unit time.Duration
}

// NewWriteClient returns a client for cfg, which must carry the write
// credential, never the read one.
func NewWriteClient(cfg Config) *WriteClient {
	return &WriteClient{c: New(cfg), unit: time.Second}
}

func (w *WriteClient) issueURL(key, rest string) string {
	return w.c.baseURL + "/rest/api/2/issue/" + url.PathEscape(key) + rest
}

func (w *WriteClient) GetIssue(ctx context.Context, key string) (WriteIssue, error) {
	var resp struct {
		Key    string `json:"key"`
		Fields struct {
			Project   struct{ Key string }  `json:"project"`
			IssueType TypeField             `json:"issuetype"`
			Status    struct{ Name string } `json:"status"`
			Target    []VersionField        `json:"customfield_10855"`
		} `json:"fields"`
	}
	q := url.Values{"fields": {"project,issuetype,status," + TargetVersionField}}
	if err := w.send(ctx, http.MethodGet, w.issueURL(key, "?"+q.Encode()), nil, &resp); err != nil {
		return WriteIssue{}, fmt.Errorf("get issue %s: %w", key, err)
	}
	issue := WriteIssue{Key: resp.Key, Project: resp.Fields.Project.Key, Type: resp.Fields.IssueType.Name, Status: resp.Fields.Status.Name}
	for _, v := range resp.Fields.Target {
		issue.TargetVersions = append(issue.TargetVersions, v.Name)
	}
	return issue, nil
}

func (w *WriteClient) ListVersions(ctx context.Context) (map[string]VersionField, error) {
	return w.c.ListVersions(ctx)
}

func (w *WriteClient) GetEditMeta(ctx context.Context, key string) (EditMeta, error) {
	var resp struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := w.send(ctx, http.MethodGet, w.issueURL(key, "/editmeta"), nil, &resp); err != nil {
		return EditMeta{}, fmt.Errorf("get editmeta %s: %w", key, err)
	}
	_, ok := resp.Fields[TargetVersionField]
	return EditMeta{TargetVersion: ok}, nil
}

func (w *WriteClient) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	var resp struct {
		Transitions []struct {
			ID   string                `json:"id"`
			Name string                `json:"name"`
			To   struct{ Name string } `json:"to"`
		} `json:"transitions"`
	}
	if err := w.send(ctx, http.MethodGet, w.issueURL(key, "/transitions"), nil, &resp); err != nil {
		return nil, fmt.Errorf("get transitions %s: %w", key, err)
	}
	var ts []Transition
	for _, t := range resp.Transitions {
		ts = append(ts, Transition{ID: t.ID, Name: t.Name, To: t.To.Name})
	}
	return ts, nil
}

// EditTargetVersions sets the issue's Target Versions to names.
func (w *WriteClient) EditTargetVersions(ctx context.Context, key string, names []string) error {
	versions := make([]map[string]string, len(names))
	for i, n := range names {
		versions[i] = map[string]string{"name": n}
	}
	body := map[string]any{"fields": map[string]any{TargetVersionField: versions}}
	if err := w.send(ctx, http.MethodPut, w.issueURL(key, ""), body, nil); err != nil {
		return fmt.Errorf("edit target versions %s: %w", key, err)
	}
	return nil
}

func (w *WriteClient) Transition(ctx context.Context, key, id string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	if err := w.send(ctx, http.MethodPost, w.issueURL(key, "/transitions"), body, nil); err != nil {
		return fmt.Errorf("transition %s: %w", key, err)
	}
	return nil
}

func (w *WriteClient) ListComments(ctx context.Context, key string) ([]Comment, error) {
	var comments []Comment
	for {
		var resp struct {
			Comments []Comment `json:"comments"`
			Total    int       `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(len(comments))}, "maxResults": {"100"}}
		if err := w.send(ctx, http.MethodGet, w.issueURL(key, "/comment?"+q.Encode()), nil, &resp); err != nil {
			return nil, fmt.Errorf("list comments %s: %w", key, err)
		}
		comments = append(comments, resp.Comments...)
		if len(resp.Comments) == 0 || len(comments) >= resp.Total {
			return comments, nil
		}
	}
}

// AddComment posts body on the issue and returns the comment's ID.
func (w *WriteClient) AddComment(ctx context.Context, key, body string) (string, error) {
	var resp Comment
	if err := w.send(ctx, http.MethodPost, w.issueURL(key, "/comment"), map[string]string{"body": body}, &resp); err != nil {
		return "", fmt.Errorf("add comment %s: %w", key, err)
	}
	return resp.ID, nil
}

// send makes one call at a time, spaced by the client's delay. A 429 or 503
// is retried after its Retry-After, else a backoff, both capped, up to
// maxRetries times.
func (w *WriteClient) send(ctx context.Context, method, reqURL string, in, out any) error {
	const (
		maxRetries = 3
		maxWait    = 60
	)
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	wait := time.Duration(0)
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(max(w.c.minDelay, wait)):
		}
		var body io.Reader
		if in != nil {
			body = bytes.NewReader(payload)
		}
		req, err := w.c.newRequest(ctx, method, reqURL, body)
		if err != nil {
			return err
		}
		resp, err := w.c.httpClient.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
			if attempt == maxRetries {
				return fmt.Errorf("JIRA API returned %d after %d retries: %s", resp.StatusCode, maxRetries, data[:min(len(data), 200)])
			}
			secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
			if err != nil || secs <= 0 {
				secs = 1 << attempt
			}
			wait = time.Duration(min(secs, maxWait)) * w.unit
			continue
		case resp.StatusCode < 200 || resp.StatusCode > 299:
			return fmt.Errorf("JIRA API returned %d: %s", resp.StatusCode, data[:min(len(data), 200)])
		case resp.Header.Get("X-Seraph-LoginReason") == "AUTHENTICATED_FAILED":
			return fmt.Errorf("JIRA API returned %d: token rejected (X-Seraph-LoginReason: AUTHENTICATED_FAILED)", http.StatusUnauthorized)
		}
		if out == nil || len(data) == 0 {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}
