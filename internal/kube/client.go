package kube

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pageSize is the limit sent on each list request.
const pageSize = "500"

// ErrUnauthorized is returned when the API server rejects the bearer token (HTTP 401).
var ErrUnauthorized = errors.New("kubeconfig credentials rejected (token expired?)")

// ErrForbidden is returned when the credentials lack permission for the request (HTTP 403).
var ErrForbidden = errors.New("kubeconfig credentials forbidden from this resource")

// Client issues authenticated, read-only list requests to a Kubernetes API server.
type Client struct {
	httpClient *http.Client
	server     string
	token      string
}

// NewClient builds a Client from a resolved Config.
func NewClient(cfg *Config) (*Client, error) {
	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipTLSVerify}
	if len(cfg.CAData) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CAData) {
			return nil, fmt.Errorf("parse certificate authority data: no certificates found")
		}
		tlsConfig.RootCAs = pool
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &Client{
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		server: strings.TrimRight(cfg.Server, "/"),
		token:  cfg.BearerToken,
	}, nil
}

// listPage is the part of a Kubernetes list response that List needs.
type listPage struct {
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items json.RawMessage `json:"items"`
}

// List GETs the list endpoint at path (e.g. /apis/<group>/<version>/namespaces/<ns>/<resource>),
// following continue tokens, and calls page with each page's raw "items" JSON array.
func (c *Client) List(ctx context.Context, path string, page func(items json.RawMessage) error) error {
	cont := ""
	for {
		list, err := c.getPage(ctx, path, cont)
		if err != nil {
			return err
		}
		if err := page(list.Items); err != nil {
			return fmt.Errorf("list %s: %w", path, err)
		}
		if list.Metadata.Continue == "" {
			return nil
		}
		cont = list.Metadata.Continue
	}
}

func (c *Client) getPage(ctx context.Context, path, cont string) (*listPage, error) {
	q := url.Values{"limit": {pageSize}}
	if cont != "" {
		q.Set("continue", cont)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.server+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The body is never included in errors: it can echo request details.
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("list %s: %w", path, ErrUnauthorized)
	case http.StatusForbidden:
		return nil, fmt.Errorf("list %s: %w", path, ErrForbidden)
	default:
		return nil, fmt.Errorf("list %s: unexpected status %d", path, resp.StatusCode)
	}

	var list listPage
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("list %s: decode: %w", path, err)
	}
	return &list, nil
}
