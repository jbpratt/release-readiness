// Package artbuild links component images to their records in the public ART
// build history service.
package artbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Client reads the ART build history service.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// searchBuild is one row of a /search listing. Pending rows have no image.
type searchBuild struct {
	NVR           string `json:"nvr"`
	RecordID      string `json:"record_id"`
	ImagePullspec string `json:"image_pullspec"`
	Outcome       string `json:"outcome"`
	Commitish     string `json:"commitish"`
	StartTime     string `json:"start_time"`
	Type          string `json:"type"`
}

// record is a full /build record.
type record struct {
	NVR           string `json:"nvr"`
	RecordID      string `json:"record_id"`
	ImagePullspec string `json:"image_pullspec"`
	Commitish     string `json:"commitish"`
	SourceRepo    string `json:"source_repo"`
}

func (c *Client) search(ctx context.Context, q url.Values) ([]searchBuild, error) {
	var resp struct {
		Builds []searchBuild `json:"builds"`
		Error  string        `json:"error"`
	}
	if err := c.get(ctx, "/search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("search: %s", resp.Error)
	}
	return resp.Builds, nil
}

func (c *Client) record(ctx context.Context, nvr, recordID string) (*record, error) {
	var r record
	if err := c.get(ctx, "/build?"+recordQuery(nvr, recordID)+"&format=json", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	// /search answers with HTML unless the request looks like the page's own XHR.
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// PageURL is a record's human build page on the service.
func PageURL(baseURL, nvr, recordID string) string {
	return strings.TrimRight(baseURL, "/") + "/build?" + recordQuery(nvr, recordID)
}

func recordQuery(nvr, recordID string) string {
	return url.Values{"nvr": {nvr}, "record_id": {recordID}}.Encode()
}
