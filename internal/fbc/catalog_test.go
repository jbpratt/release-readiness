package fbc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

var wantBundles = []Bundle{
	{"quay-operator", "stable-3.18", "quay-operator.v3.18.0", "registry.redhat.io/quay/quay-operator-bundle@sha256:030ee454a0e8ae48d1c934128f554ff0b83afe1ca1803cdedfc14bb28a62d019", "sha256:030ee454a0e8ae48d1c934128f554ff0b83afe1ca1803cdedfc14bb28a62d019"},
	{"quay-operator", "stable-3.18", "quay-operator.v3.18.1", "registry.redhat.io/quay/quay-operator-bundle@sha256:b241d01c6f2171ead9cd1060dfd5cd0000c426eb62e475e00565c2796de881ba", "sha256:b241d01c6f2171ead9cd1060dfd5cd0000c426eb62e475e00565c2796de881ba"},
	{"quay-operator", "stable-3.17", "quay-operator.v3.17.9", "registry.redhat.io/quay/quay-operator-bundle:v3.17.9", ""},
}

func TestParseCatalog(t *testing.T) {
	f, err := os.Open("testdata/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	got, err := ParseCatalog(f)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, wantBundles) {
		t.Errorf("bundles = %+v, want %+v", got, wantBundles)
	}

	for name, in := range map[string]string{
		"invalid yaml":   "schema: olm.channel\nentries: [\n",
		"missing bundle": "schema: olm.channel\nname: stable-3.18\npackage: quay-operator\nentries:\n- name: quay-operator.v3.18.2\n",
	} {
		if _, err := ParseCatalog(strings.NewReader(in)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

// registry serves one image index whose amd64 manifest has a base layer and a
// top layer holding files, and returns a client for it and the image pullspec.
func registry(t *testing.T, files map[string]string) (*Client, string) {
	t.Helper()
	blobs := map[string][]byte{}
	put := func(b []byte) string {
		d := sha256Digest(b)
		blobs[d] = b
		return d
	}
	layer := func(files map[string]string) []byte {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		for name, body := range files {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	desc := func(mediaType string, b []byte) map[string]any {
		return map[string]any{"mediaType": mediaType, "digest": put(b), "size": len(b)}
	}
	marshal := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	const gzLayer = "application/vnd.oci.image.layer.v1.tar+gzip"
	amd64 := marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        desc("application/vnd.oci.image.config.v1+json", []byte("{}")),
		"layers": []any{
			desc(gzLayer, layer(map[string]string{"usr/bin/opm": "binary"})),
			desc(gzLayer, layer(files)),
		},
	})
	arm64 := marshal(map[string]any{"schemaVersion": 2, "layers": []any{}})
	platform := func(arch string, m []byte) map[string]any {
		d := desc("application/vnd.oci.image.manifest.v1+json", m)
		d["platform"] = map[string]string{"architecture": arch, "os": "linux"}
		return d
	}
	index := put(marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests":     []any{platform("arm64", arm64), platform("amd64", amd64)},
	}))

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/v2/ocp-art-tenant/art-fbc/"
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		kind, digest, _ := strings.Cut(rest, "/")
		b, found := blobs[digest]
		switch {
		case !ok || !found:
			http.NotFound(w, r)
		case kind == "blobs" && r.URL.Query().Get("cdn") == "":
			// Blobs redirect to storage, as quay.io's do.
			http.Redirect(w, r, r.URL.Path+"?cdn=1", http.StatusTemporaryRedirect)
		default:
			_, _ = w.Write(b)
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.Client()), strings.TrimPrefix(srv.URL, "https://") + "/ocp-art-tenant/art-fbc@" + index
}

func TestBundles(t *testing.T) {
	catalog, err := os.ReadFile("testdata/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, image := registry(t, map[string]string{"./configs/quay-operator/catalog.yaml": string(catalog)})
	got, err := c.Bundles(t.Context(), image)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, wantBundles) {
		t.Errorf("bundles = %+v, want %+v", got, wantBundles)
	}

	repo, _, _ := strings.Cut(image, "@")
	for name, image := range map[string]string{
		"tag reference":  repo + ":latest",
		"unknown digest": repo + "@sha256:" + strings.Repeat("0", 64),
	} {
		if _, err := c.Bundles(t.Context(), image); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
	for name, files := range map[string]map[string]string{
		"parse error": {"configs/quay-operator/catalog.yaml": "entries: ["},
		"no catalog":  {"configs/other/catalog.yaml": string(catalog)},
	} {
		c, image := registry(t, files)
		if _, err := c.Bundles(t.Context(), image); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}
