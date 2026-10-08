// Package catalog reads which image tags the public Red Hat container catalog
// has published, the signal that a release version has shipped.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

// Repositories lists the registry.access.redhat.com repositories that publish
// each product. Quay 3.9-3.15 publish to quay-rhel8.
var Repositories = map[string][]string{
	"quay": {"quay/quay-rhel9", "quay/quay-rhel8"},
	"omr":  {"openshift/mirror-registry-rhel8"},
}

// Client reads the catalog API.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

type imagesPage struct {
	Total int `json:"total"`
	Data  []struct {
		Repositories []struct {
			Published bool `json:"published"`
			Tags      []struct {
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"repositories"`
	} `json:"data"`
}

// PublishedTags returns every tag of a published image in repository.
func (c *Client) PublishedTags(ctx context.Context, repository string) ([]string, error) {
	var tags []string
	for page, seen := 0, 0; ; page++ {
		q := url.Values{
			"page_size": {"500"},
			"page":      {strconv.Itoa(page)},
			"include":   {"total,data.repositories.published,data.repositories.tags.name"},
		}
		path := "/repositories/registry/registry.access.redhat.com/repository/" + repository + "/images?" + q.Encode()
		var p imagesPage
		if err := c.get(ctx, path, &p); err != nil {
			return nil, err
		}
		for _, img := range p.Data {
			for _, r := range img.Repositories {
				if !r.Published {
					continue
				}
				for _, t := range r.Tags {
					tags = append(tags, t.Name)
				}
			}
		}
		seen += len(p.Data)
		if len(p.Data) == 0 || seen >= p.Total {
			return tags, nil
		}
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
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Shipped holds the published versions of each product in memory, refreshed
// in the background so requests never wait on the catalog.
type Shipped struct {
	client   *Client
	logger   *slog.Logger
	mu       sync.RWMutex
	versions map[string]bool // "product X.Y.Z"
	// Status receives each refresh's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

func NewShipped(client *Client, logger *slog.Logger) *Shipped {
	return &Shipped{client: client, logger: logger}
}

// Run refreshes immediately and then every interval until ctx is cancelled.
func (s *Shipped) Run(ctx context.Context, interval time.Duration) {
	s.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Refresh(ctx)
		}
	}
}

// Refresh replaces the published set only when every repository was read,
// so a failed pass keeps the last good one.
func (s *Shipped) Refresh(ctx context.Context) {
	versions := make(map[string]bool)
	for product, repos := range Repositories {
		for _, repo := range repos {
			tags, err := s.client.PublishedTags(ctx, repo)
			if err != nil {
				s.logger.Warn("catalog refresh", "repository", repo, "error", err)
				s.Status.Report(fmt.Errorf("read %s: %w", repo, err))
				return
			}
			// OMR tags drop the v prefix from 2.0.9 on.
			for _, t := range tags {
				versions[product+" "+strings.TrimPrefix(t, "v")] = true
			}
		}
	}
	s.mu.Lock()
	s.versions = versions
	s.mu.Unlock()
	s.Status.Report(nil)
	s.logger.Info("catalog refreshed", "tags", len(versions))
}

// Has reports whether product version X.Y.Z is published. A nil Shipped
// (catalog disabled) has nothing published.
func (s *Shipped) Has(product, version string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.versions[product+" "+version]
}
