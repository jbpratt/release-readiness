package fbc

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/artbuild"
)

const (
	// maxJSON bounds an image index, manifest or config.
	maxJSON = 4 << 20
	// nvrLabel is the label naming the ART build of an image.
	nvrLabel = "com.redhat.art.nvr"
)

// Registry reads FBC images by digest over the Registry v2 HTTP API,
// anonymously.
type Registry struct {
	http *http.Client
	// maxLayer and maxExpanded bound a layer's compressed and uncompressed
	// bytes: a catalog layer is MBs, base image layers are hundreds.
	maxLayer, maxExpanded int64
}

func NewRegistry(hc *http.Client) *Registry {
	return &Registry{http: hc, maxLayer: 64 << 20, maxExpanded: 256 << 20}
}

// Image is what an FBC image says of itself: the ART build its label names,
// and the channel for the label's X.Y stream of the package of its /configs.
type Image struct {
	LabelNVR string
	Channel  Channel
}

type descriptor struct {
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	Platform struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

// manifest is an image index, with Manifests, or an image manifest.
type manifest struct {
	Manifests []descriptor `json:"manifests"`
	Config    descriptor   `json:"config"`
	Layers    []descriptor `json:"layers"`
}

// Read renders the linux/amd64 image of pullspec, a host/repository@digest.
// On error, Image holds what was read before it.
func (r *Registry) Read(ctx context.Context, pullspec string) (Image, error) {
	var img Image
	repo, digest, ok := strings.Cut(pullspec, "@")
	host, name, ok2 := strings.Cut(repo, "/")
	if !ok || !ok2 {
		return img, fmt.Errorf("pullspec %q is not host/repository@digest", pullspec)
	}
	base := "https://" + host + "/v2/" + name
	var m manifest
	if err := r.getJSON(ctx, base+"/manifests/"+digest, digest, &m); err != nil {
		return img, err
	}
	if len(m.Manifests) > 0 {
		i := slices.IndexFunc(m.Manifests, func(d descriptor) bool { return d.Platform.OS == "linux" && d.Platform.Architecture == "amd64" })
		if i < 0 {
			return img, errors.New("no linux/amd64 image in the index")
		}
		if err := r.getJSON(ctx, base+"/manifests/"+m.Manifests[i].Digest, m.Manifests[i].Digest, &m); err != nil {
			return img, err
		}
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := r.getJSON(ctx, base+"/blobs/"+m.Config.Digest, m.Config.Digest, &cfg); err != nil {
		return img, err
	}
	img.LabelNVR = cfg.Config.Labels[nvrLabel]
	_, version, _, ok := artbuild.ParseFBC(img.LabelNVR)
	if !ok {
		return img, fmt.Errorf("label %s=%q is not an FBC NVR", nvrLabel, img.LabelNVR)
	}
	channel := "stable-" + version[:strings.LastIndex(version, ".")]
	// The catalog is added last, so the newest layer holds it.
	for i := len(m.Layers) - 1; i >= 0; i-- {
		pkg, ch, err := r.readLayer(ctx, base, m.Layers[i], channel)
		if err != nil || pkg != "" {
			img.Channel = ch
			return img, err
		}
	}
	return img, errors.New("no /configs/<package>/catalog.yaml in any layer")
}

// readLayer returns the package of the layer's /configs catalog and its
// channel's head, or no package if the layer has no catalog.
func (r *Registry) readLayer(ctx context.Context, base string, l descriptor, channel string) (string, Channel, error) {
	if l.Size > r.maxLayer {
		return "", Channel{}, fmt.Errorf("layer %s of %d bytes exceeds the %d byte bound", l.Digest, l.Size, r.maxLayer)
	}
	body, err := r.get(ctx, base+"/blobs/"+l.Digest)
	if err != nil {
		return "", Channel{}, err
	}
	defer func() { _ = body.Close() }()
	h := sha256.New()
	compressed := io.TeeReader(&bounded{r: body, n: r.maxLayer, what: "layer " + l.Digest}, h)
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return "", Channel{}, fmt.Errorf("layer %s: %w", l.Digest, err)
	}
	tr := tar.NewReader(&bounded{r: gz, n: r.maxExpanded, what: "expanded layer " + l.Digest})
	var pkg string
	var ch Channel
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", Channel{}, fmt.Errorf("layer %s: %w", l.Digest, err)
		}
		name := strings.TrimPrefix(path.Clean("/"+hdr.Name), "/")
		if ok, _ := path.Match("configs/*/catalog.yaml", name); !ok {
			continue
		}
		if pkg != "" {
			return "", Channel{}, fmt.Errorf("layer %s holds catalogs of %s and %s", l.Digest, pkg, path.Base(path.Dir(name)))
		}
		pkg = path.Base(path.Dir(name))
		if ch, err = ParseChannel(tr, channel); err != nil {
			return pkg, Channel{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return "", Channel{}, fmt.Errorf("layer %s: %w", l.Digest, err)
	}
	if got := fmt.Sprintf("sha256:%x", h.Sum(nil)); got != l.Digest {
		return "", Channel{}, fmt.Errorf("layer %s has digest %s", l.Digest, got)
	}
	return pkg, ch, nil
}

// getJSON decodes the document at url, after checking it has digest.
func (r *Registry) getJSON(ctx context.Context, url, digest string, v any) error {
	body, err := r.get(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(&bounded{r: body, n: maxJSON, what: url})
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("sha256:%x", sha256.Sum256(data)); got != digest {
		return fmt.Errorf("GET %s: digest %s, want %s", url, got, digest)
	}
	return json.Unmarshal(data, v)
}

func (r *Registry) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// bounded reads r, failing once it has given more than n bytes.
type bounded struct {
	r    io.Reader
	n    int64
	what string
}

func (b *bounded) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if b.n -= int64(n); b.n < 0 {
		return n, fmt.Errorf("%s exceeds its size bound", b.what)
	}
	return n, err
}
