// Package fbc reads the bundles a file-based catalog (FBC) image references,
// straight from its anonymous registry v2 API.
package fbc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	Package = "quay-operator"

	StateParsed = "parsed"
	StateFailed = "failed"

	catalogPath = "configs/quay-operator/catalog.yaml"
	maxManifest = 4 << 20
	// The catalog is in the small top layer; the base layers below it run to
	// hundreds of MiB, so a catalog not found by then is not read at all.
	maxLayer   = 32 << 20
	maxCatalog = 32 << 20
)

// Bundle is one channel entry with the bundle image its olm.bundle names.
// Digest is empty unless Image is pinned by sha256 digest.
type Bundle struct {
	Package, Channel, Name, Image, Digest string
}

// Catalog is the stored read of the newest FBC Snapshot's catalog image. State
// is empty while unread; Bundles holds one channel's entries.
type Catalog struct {
	Snapshot, State string
	Bundles         []Bundle
}

// ParseCatalog returns every channel entry of a multi-document file-based
// catalog. An entry without its olm.bundle fails the parse.
func ParseCatalog(r io.Reader) ([]Bundle, error) {
	type doc struct {
		Schema  string `yaml:"schema"`
		Name    string `yaml:"name"`
		Package string `yaml:"package"`
		Image   string `yaml:"image"`
		Entries []struct {
			Name string `yaml:"name"`
		} `yaml:"entries"`
	}
	images := map[[2]string]string{}
	var channels []doc
	dec := yaml.NewDecoder(r)
	for {
		var d doc
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse catalog: %w", err)
		}
		switch d.Schema {
		case "olm.bundle":
			images[[2]string{d.Package, d.Name}] = d.Image
		case "olm.channel":
			channels = append(channels, d)
		}
	}
	var bundles []Bundle
	for _, c := range channels {
		for _, e := range c.Entries {
			img, ok := images[[2]string{c.Package, e.Name}]
			if !ok {
				return nil, fmt.Errorf("channel %s entry %s has no olm.bundle", c.Name, e.Name)
			}
			b := Bundle{Package: c.Package, Channel: c.Name, Name: e.Name, Image: img}
			if _, d, _ := strings.Cut(img, "@"); strings.HasPrefix(d, "sha256:") {
				b.Digest = d
			}
			bundles = append(bundles, b)
		}
	}
	return bundles, nil
}

// Client reads catalog images over HTTPS, following blob redirects.
type Client struct {
	http *http.Client
}

func NewClient(h *http.Client) *Client {
	return &Client{http: h}
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  struct {
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

type manifest struct {
	Manifests []descriptor `json:"manifests"`
	Layers    []descriptor `json:"layers"`
}

// Bundles reads catalog.yaml from the digest-pinned image, taking the amd64
// image of an index: the catalog is the same data on every platform.
func (c *Client) Bundles(ctx context.Context, image string) ([]Bundle, error) {
	name, digest, _ := strings.Cut(image, "@")
	host, repo, ok := strings.Cut(name, "/")
	if !ok || !strings.HasPrefix(digest, "sha256:") {
		return nil, fmt.Errorf("%s: not a digest-pinned image", image)
	}
	repo, _, _ = strings.Cut(repo, ":")
	base := "https://" + host + "/v2/" + repo
	m, err := c.manifest(ctx, base, digest)
	if err != nil {
		return nil, err
	}
	if len(m.Manifests) > 0 {
		i := max(slices.IndexFunc(m.Manifests, func(d descriptor) bool { return d.Platform.Architecture == "amd64" }), 0)
		if m, err = c.manifest(ctx, base, m.Manifests[i].Digest); err != nil {
			return nil, err
		}
	}
	for _, l := range slices.Backward(m.Layers) {
		data, err := c.findInLayer(ctx, base, l)
		if err != nil {
			return nil, err
		}
		if data != nil {
			return ParseCatalog(bytes.NewReader(data))
		}
	}
	return nil, fmt.Errorf("%s: no %s", image, catalogPath)
}

func (c *Client) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (c *Client) manifest(ctx context.Context, base, digest string) (*manifest, error) {
	resp, err := c.get(ctx, base+"/manifests/"+digest, strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", digest, err)
	}
	if len(body) > maxManifest {
		return nil, fmt.Errorf("manifest %s exceeds %d bytes", digest, maxManifest)
	}
	if got := sha256Digest(body); got != digest {
		return nil, fmt.Errorf("manifest %s has digest %s", digest, got)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", digest, err)
	}
	return &m, nil
}

// findInLayer returns catalog.yaml from a gzip tar layer, or nil if the layer
// lacks it. The whole blob is read to check its digest.
func (c *Client) findInLayer(ctx context.Context, base string, l descriptor) ([]byte, error) {
	if !strings.HasSuffix(l.MediaType, "tar+gzip") && !strings.HasSuffix(l.MediaType, "tar.gzip") {
		return nil, fmt.Errorf("layer %s: unsupported media type %q", l.Digest, l.MediaType)
	}
	if l.Size > maxLayer {
		return nil, fmt.Errorf("layer %s: %d bytes exceeds %d before %s was found", l.Digest, l.Size, maxLayer, catalogPath)
	}
	resp, err := c.get(ctx, base+"/blobs/"+l.Digest, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	h := sha256.New()
	blob := io.TeeReader(io.LimitReader(resp.Body, l.Size+1), h)
	zr, err := gzip.NewReader(blob)
	if err != nil {
		return nil, fmt.Errorf("layer %s: %w", l.Digest, err)
	}
	var data []byte
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", l.Digest, err)
		}
		if hdr.Typeflag != tar.TypeReg || strings.TrimPrefix(path.Clean("/"+hdr.Name), "/") != catalogPath {
			continue
		}
		if hdr.Size > maxCatalog {
			return nil, fmt.Errorf("layer %s: %s is %d bytes, over %d", l.Digest, catalogPath, hdr.Size, maxCatalog)
		}
		if data, err = io.ReadAll(tr); err != nil {
			return nil, fmt.Errorf("layer %s: read %s: %w", l.Digest, catalogPath, err)
		}
	}
	if _, err := io.Copy(io.Discard, blob); err != nil {
		return nil, fmt.Errorf("layer %s: %w", l.Digest, err)
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != l.Digest {
		return nil, fmt.Errorf("layer %s has digest %s", l.Digest, got)
	}
	return data, nil
}

func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
