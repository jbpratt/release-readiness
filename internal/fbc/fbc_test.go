package fbc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

// catalog is a 3.16 FBC's quay-operator catalog: the package defaults to the
// newest stream's channel, and stable-3.16 heads at a placeholder version
// whose bundle lists fewer related images than the v3.16.6 below it.
const catalog = `---
defaultChannel: stable-3.18
name: quay-operator
schema: olm.package
---
entries:
- name: quay-operator.v3.16.6
  skipRange: '>=3.13.0 <3.16.6'
- name: quay-operator.v3.16.5
- name: quay-operator.v3.99.0-dev
  replaces: quay-operator.v3.16.6
  skips:
  - quay-operator.v3.16.5
  skipRange: '>=3.13.0 <3.99.0-dev'
name: stable-3.16
package: quay-operator
schema: olm.channel
---
entries:
- name: quay-operator.v3.18.1
name: stable-3.18
package: quay-operator
schema: olm.channel
---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:1606
name: quay-operator.v3.16.6
package: quay-operator
properties:
- type: olm.gvk
  value:
    group: quay.redhat.com
    kind: QuayRegistry
    version: v1
- type: olm.package
  value:
    packageName: quay-operator
    version: 3.16.6
relatedImages:
- image: registry.redhat.io/quay/quay-operator-rhel9@sha256:o1606
  name: quay-operator
- image: registry.redhat.io/rhel9/postgresql-15@sha256:p15
  name: postgres
schema: olm.bundle
---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:1605
name: quay-operator.v3.16.5
package: quay-operator
properties:
- type: olm.package
  value:
    packageName: quay-operator
    version: 3.16.5
schema: olm.bundle
---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:1699
name: quay-operator.v3.99.0-dev
package: quay-operator
properties:
- type: olm.package
  value:
    packageName: quay-operator
    version: 3.99.0-dev
relatedImages:
- image: registry.redhat.io/quay/quay-rhel9-operator@sha256:o1699
  name: quay-operator
schema: olm.bundle
---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:1801
name: quay-operator.v3.18.1
package: quay-operator
properties:
- type: olm.package
  value:
    packageName: quay-operator
    version: 3.18.1
schema: olm.bundle
`

var wantHead = Channel{
	Name:               "stable-3.16",
	HeadCSV:            "quay-operator.v3.99.0-dev",
	HeadBundlePullspec: "registry.redhat.io/quay/quay-operator-bundle@sha256:1699",
	HeadRelatedImages:  []RelatedImage{{Name: "quay-operator", Image: "registry.redhat.io/quay/quay-rhel9-operator@sha256:o1699"}},
	Previous: Bundle{
		CSV:            "quay-operator.v3.16.6",
		Version:        "3.16.6",
		BundlePullspec: "registry.redhat.io/quay/quay-operator-bundle@sha256:1606",
		RelatedImages: []RelatedImage{
			{Name: "quay-operator", Image: "registry.redhat.io/quay/quay-operator-rhel9@sha256:o1606"},
			{Name: "postgres", Image: "registry.redhat.io/rhel9/postgresql-15@sha256:p15"},
		},
	},
}

func TestParseChannel(t *testing.T) {
	got, err := ParseChannel(strings.NewReader(catalog), "stable-3.16")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantHead) {
		t.Errorf("got %+v, want %+v", got, wantHead)
	}
	// A channel's only entry has no previous one.
	got, err = ParseChannel(strings.NewReader(catalog), "stable-3.18")
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadCSV != "quay-operator.v3.18.1" || !reflect.DeepEqual(got.Previous, Bundle{}) {
		t.Errorf("stable-3.18: head %q, previous %+v", got.HeadCSV, got.Previous)
	}
}

// A channel without exactly one head, or whose head has no bundle image, has
// no bundle to ship.
func TestParseChannelErrors(t *testing.T) {
	const bundles = `---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:a
name: a
schema: olm.bundle
---
image: registry.redhat.io/quay/quay-operator-bundle@sha256:b
name: b
schema: olm.bundle
`
	for name, tc := range map[string]struct{ entries, want string }{
		"two heads":  {"- name: a\n- name: b\n", "has 2 heads"},
		"zero heads": {"- name: a\n  replaces: b\n- name: b\n  replaces: a\n", "has 0 heads"},
		"no bundle":  {"- name: c\n", "head c has no bundle image"},
	} {
		cat := "---\nentries:\n" + tc.entries + "name: stable-3.16\nschema: olm.channel\n" + bundles
		if _, err := ParseChannel(strings.NewReader(cat), "stable-3.16"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
}

func digestOf(b []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

func targz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeRegistry serves one FBC image, a multi-arch index whose linux/amd64
// image has a base layer and a catalog layer. It counts GETs by digest and
// fails the next fail GETs of the catalog layer.
type fakeRegistry struct {
	mu       sync.Mutex
	docs     map[string][]byte
	gets     map[string]int
	fail     int
	pullspec string
	base     string
	layer    string
}

func newFakeRegistry(t *testing.T) (*fakeRegistry, *httptest.Server) {
	f := &fakeRegistry{docs: map[string][]byte{}, gets: map[string]int{}}
	add := func(b []byte) string {
		d := digestOf(b)
		f.docs[d] = b
		return d
	}
	base := targz(t, map[string]string{"etc/os-release": "ID=rhel\n"})
	layer := targz(t, map[string]string{"configs/quay-operator/catalog.yaml": catalog, "root/buildinfo/labels.json": "{}"})
	f.base, f.layer = add(base), add(layer)
	config := add(mustJSON(t, map[string]any{"config": map[string]any{"Labels": map[string]string{nvrLabel: "quay-operator-fbc-3.16.7-20261012191500.ocp4.21"}}}))
	amd64 := add(mustJSON(t, map[string]any{
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config":    map[string]any{"digest": config},
		"layers":    []map[string]any{{"digest": f.base, "size": len(base)}, {"digest": f.layer, "size": len(layer)}},
	}))
	index := add(mustJSON(t, map[string]any{
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": []map[string]any{
			{"digest": "sha256:arm64", "platform": map[string]string{"architecture": "arm64", "os": "linux"}},
			{"digest": amd64, "platform": map[string]string{"architecture": "amd64", "os": "linux"}},
		},
	}))
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	f.pullspec = srv.Listener.Addr().String() + "/ocp-art-tenant/art-fbc@" + index
	return f, srv
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.gets[d]++
	if d == f.layer && f.fail > 0 {
		f.fail--
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	b, ok := f.docs[d]
	if !strings.HasPrefix(r.URL.Path, "/v2/ocp-art-tenant/art-fbc/") || !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(b)
}

func (f *fakeRegistry) count(digest string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets[digest]
}

// memStore keeps renders as the database does. Its ART bundle builds are
// the groups CloseBundle was asked for, by bundle digest.
type memStore struct {
	pullspecs []string
	renders   map[string]Render
	closed    map[string]string
}

func (m *memStore) FBCsToRender(_ context.Context, now time.Time, limit int) ([]string, error) {
	var out []string
	for _, ps := range m.pullspecs {
		r, ok := m.renders[ps[strings.Index(ps, "@")+1:]]
		if !ok || r.State == StateFailed && !r.RetryAfter.After(now) {
			out = append(out, ps)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (m *memStore) UpsertFBCRender(_ context.Context, r Render) error {
	if m.renders[r.Digest].State != StateRendered {
		m.renders[r.Digest] = r
	}
	return nil
}

func (m *memStore) UnjoinedFBCRenders(context.Context) ([]Render, error) {
	var out []Render
	for _, r := range m.renders {
		if _, ok := m.closed[r.Channel.HeadBundlePullspec[strings.Index(r.Channel.HeadBundlePullspec, "@")+1:]]; r.State == StateRendered && !ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) CloseBundle(_ context.Context, group, digest string) error {
	m.closed[digest] = group
	return nil
}

func newTestSync(t *testing.T) (*Sync, *fakeRegistry, *memStore, *time.Time) {
	f, srv := newFakeRegistry(t)
	store := &memStore{pullspecs: []string{f.pullspec}, renders: map[string]Render{}, closed: map[string]string{}}
	s := NewSync(NewRegistry(srv.Client()), store, store, slog.New(slog.DiscardHandler))
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, f, store, &now
}

func TestSyncCachesByDigest(t *testing.T) {
	s, f, store, _ := newTestSync(t)
	s.SyncOnce(t.Context())
	s.SyncOnce(t.Context())

	digest := f.pullspec[strings.Index(f.pullspec, "@")+1:]
	r := store.renders[digest]
	if r.State != StateRendered || !reflect.DeepEqual(r.Channel, wantHead) || r.LabelNVR != "quay-operator-fbc-3.16.7-20261012191500.ocp4.21" {
		t.Errorf("render: %+v", r)
	}
	if n := f.count(f.layer); n != 1 {
		t.Errorf("catalog layer downloaded %d times, want 1", n)
	}
	if n := f.count(f.base); n != 0 {
		t.Errorf("base layer downloaded %d times, want 0", n)
	}
	if want := map[string]string{"sha256:1699": "quay-3.16"}; !maps.Equal(store.closed, want) {
		t.Errorf("closed bundles %v, want %v", store.closed, want)
	}
}

func TestSyncRetriesFailedRender(t *testing.T) {
	s, f, store, now := newTestSync(t)
	status := syncstatus.New()
	s.Status = status.Track("fbc-render", 0)
	f.fail = 1
	s.SyncOnce(t.Context())
	digest := f.pullspec[strings.Index(f.pullspec, "@")+1:]
	if r := store.renders[digest]; r.State != StateFailed || !strings.Contains(r.Error, "503") || !r.RetryAfter.Equal(now.Add(retryAfter)) {
		t.Fatalf("after a failed download: %+v", r)
	}
	if p := status.Problems(*now); len(p) != 1 || !strings.Contains(p[0].Message, "503") {
		t.Errorf("sync status after a failed download: %+v", p)
	}

	s.SyncOnce(t.Context())
	if n := f.count(f.layer); n != 1 {
		t.Errorf("catalog layer requested %d times before the retry time, want 1", n)
	}

	*now = now.Add(retryAfter)
	s.SyncOnce(t.Context())
	if r := store.renders[digest]; r.State != StateRendered || !reflect.DeepEqual(r.Channel, wantHead) {
		t.Errorf("after the retry: %+v", r)
	}
}

func TestSyncSizeBound(t *testing.T) {
	s, f, store, _ := newTestSync(t)
	s.registry.maxExpanded = 1 << 10
	s.SyncOnce(t.Context())
	digest := f.pullspec[strings.Index(f.pullspec, "@")+1:]
	if r := store.renders[digest]; r.State != StateFailed || !strings.Contains(r.Error, "exceeds its size bound") {
		t.Errorf("render: %+v", r)
	}
}
