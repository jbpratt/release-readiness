package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// TargetVersionField is the PROJQUAY Target Version custom field.
const TargetVersionField = "customfield_10855"

// WriteIssue is an issue's live state, as the FBC writer needs it.
type WriteIssue struct {
	Project, Status string
	TargetVersions  []string
}

// Transition is a workflow transition offered on an issue.
type Transition struct {
	ID string
	// To is the status the transition moves the issue to.
	To string
}

type Comment struct {
	Body string `json:"body"`
}

// issueURL is a v2 API URL: its comment bodies are plain text, not ADF.
func (c *Client) issueURL(key, rest string) string {
	return c.baseURL + "/rest/api/2/issue/" + url.PathEscape(key) + rest
}

func (c *Client) getJSON(ctx context.Context, reqURL string, out any) error {
	body, err := c.doGetWithRetry(ctx, reqURL)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// send makes one write call, never retried: the FBC writer retries the
// action, re-reading the issue first, so a write that went through is not
// made twice.
func (c *Client) send(ctx context.Context, method, reqURL string, in any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, method, reqURL, bytes.NewReader(payload))
	return err
}

func (c *Client) GetIssue(ctx context.Context, key string) (WriteIssue, error) {
	var resp struct {
		Fields struct {
			Project struct{ Key string }  `json:"project"`
			Status  struct{ Name string } `json:"status"`
			Target  []VersionField        `json:"customfield_10855"`
		} `json:"fields"`
	}
	q := url.Values{"fields": {"project,status," + TargetVersionField}}
	if err := c.getJSON(ctx, c.issueURL(key, "?"+q.Encode()), &resp); err != nil {
		return WriteIssue{}, fmt.Errorf("get issue %s: %w", key, err)
	}
	issue := WriteIssue{Project: resp.Fields.Project.Key, Status: resp.Fields.Status.Name}
	for _, v := range resp.Fields.Target {
		issue.TargetVersions = append(issue.TargetVersions, v.Name)
	}
	return issue, nil
}

// TargetVersionEditable reports whether the issue's Target Version can be
// edited.
func (c *Client) TargetVersionEditable(ctx context.Context, key string) (bool, error) {
	var resp struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := c.getJSON(ctx, c.issueURL(key, "/editmeta"), &resp); err != nil {
		return false, fmt.Errorf("get editmeta %s: %w", key, err)
	}
	_, ok := resp.Fields[TargetVersionField]
	return ok, nil
}

func (c *Client) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	var resp struct {
		Transitions []struct {
			ID string                `json:"id"`
			To struct{ Name string } `json:"to"`
		} `json:"transitions"`
	}
	if err := c.getJSON(ctx, c.issueURL(key, "/transitions"), &resp); err != nil {
		return nil, fmt.Errorf("get transitions %s: %w", key, err)
	}
	var ts []Transition
	for _, t := range resp.Transitions {
		ts = append(ts, Transition{ID: t.ID, To: t.To.Name})
	}
	return ts, nil
}

// EditTargetVersions adds one Target Version and removes another. Update
// ops leave the issue's other Target Versions alone, even ones set since it
// was read.
func (c *Client) EditTargetVersions(ctx context.Context, key, add, remove string) error {
	body := map[string]any{"update": map[string]any{TargetVersionField: []map[string]any{
		{"add": map[string]string{"name": add}},
		{"remove": map[string]string{"name": remove}},
	}}}
	if err := c.send(ctx, http.MethodPut, c.issueURL(key, ""), body); err != nil {
		return fmt.Errorf("edit target versions %s: %w", key, err)
	}
	return nil
}

func (c *Client) Transition(ctx context.Context, key, id string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	if err := c.send(ctx, http.MethodPost, c.issueURL(key, "/transitions"), body); err != nil {
		return fmt.Errorf("transition %s: %w", key, err)
	}
	return nil
}

func (c *Client) ListComments(ctx context.Context, key string) ([]Comment, error) {
	var comments []Comment
	for {
		var resp struct {
			Comments []Comment `json:"comments"`
			Total    int       `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(len(comments))}, "maxResults": {"100"}}
		if err := c.getJSON(ctx, c.issueURL(key, "/comment?"+q.Encode()), &resp); err != nil {
			return nil, fmt.Errorf("list comments %s: %w", key, err)
		}
		comments = append(comments, resp.Comments...)
		if len(resp.Comments) == 0 || len(comments) >= resp.Total {
			return comments, nil
		}
	}
}

func (c *Client) AddComment(ctx context.Context, key, body string) error {
	if err := c.send(ctx, http.MethodPost, c.issueURL(key, "/comment"), map[string]string{"body": body}); err != nil {
		return fmt.Errorf("add comment %s: %w", key, err)
	}
	return nil
}
