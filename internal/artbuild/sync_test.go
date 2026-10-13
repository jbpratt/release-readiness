package artbuild

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testDay    = "2026-10-12"
	qoBundle   = "quay-operator-metadata-container-3.17.6.202610120936.p2.g3c4984a.assembly.stream.el9-1"
	csoBundle  = "container-security-operator-metadata-container-3.17.6.202610120936.p2.g8239e43.assembly.stream.el9-1"
	qoImage    = "quay-operator-container-3.17.6-202610120936.p2.g3c4984a.assembly.stream.el9"
	clairImage = "quay-clair-container-3.17.6-202610110800.p2.g6bc3daf.assembly.stream.el9"
	csoImage   = "container-security-operator-container-3.17.6-202610120936.p2.g8239e43.assembly.stream.el9"
	qoFBC      = "quay-operator-fbc-3.17.6-20261012100000.ocp4.21"
	csoFBC     = "container-security-operator-fbc-3.17.6-20261012100000.ocp4.21"
)

// fakeART serves /search and /build from its successful records. Every
// record also lists as a pending row of the same NVR without an image, as
// the service lists a build that finished.
type fakeART struct {
	records []record
	// days lists, by name filter, the rows of the testDay FBC listing.
	days map[string][]searchBuild
	// searches is every /search query, in order.
	searches []url.Values
}

func pullspec(nvr string) string {
	return fmt.Sprintf("quay.io/redhat-user-workloads/ocp-art-tenant/art-images@sha256:%x", sha256.Sum256([]byte(nvr)))
}

func successRecord(nvr string) record {
	return record{NVR: nvr, RecordID: "ok-" + nvr, ImagePullspec: pullspec(nvr), Outcome: "success", StartTime: "2026-10-12T10:00:00"}
}

func (f *fakeART) rows(match func(record) bool) []searchBuild {
	var rows []searchBuild
	for _, r := range f.records {
		if match(r) {
			rows = append(rows,
				searchBuild{NVR: r.NVR, RecordID: "pending-" + r.NVR, Outcome: "pending"},
				searchBuild{NVR: r.NVR, RecordID: r.RecordID, ImagePullspec: r.ImagePullspec, Outcome: "success"})
		}
	}
	return rows
}

func (f *fakeART) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch r.URL.Path {
	case "/search":
		f.searches = append(f.searches, q)
		var rows []searchBuild
		switch {
		case q.Has("nvr"):
			rows = f.rows(func(rec record) bool { return rec.NVR == q.Get("nvr") })
		case q.Has("image_sha_tag"):
			rows = f.rows(func(rec record) bool { return strings.HasSuffix(rec.ImagePullspec, q.Get("image_sha_tag")) })
		case q.Get("dateRange") == testDay+" to "+testDay:
			rows = f.days[q.Get("name")]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"builds": rows, "warnings": []string{}})
	case "/build":
		for _, rec := range f.records {
			if rec.RecordID == q.Get("record_id") {
				_ = json.NewEncoder(w).Encode(rec)
				return
			}
		}
		// The service answers a pending record with no image.
		_ = json.NewEncoder(w).Encode(record{NVR: q.Get("nvr"), RecordID: q.Get("record_id"), Outcome: "pending"})
	default:
		http.NotFound(w, r)
	}
}

type fakeHistoryStore struct {
	builds  []HistoryBuild
	scanned []string
}

func (s *fakeHistoryStore) ListSnapshotApplications(context.Context) ([]string, error) {
	return []string{"fbc-quay-3-17", "quay-images-base"}, nil
}

func (s *fakeHistoryStore) ScannedArtHistoryDays(_ context.Context, group, buildType string) ([]string, error) {
	return nil, nil
}

func (s *fakeHistoryStore) MarkArtHistoryDayScanned(_ context.Context, group, buildType, day string, _ time.Time) error {
	s.scanned = append(s.scanned, group+" "+buildType+" "+day)
	return nil
}

func (s *fakeHistoryStore) has(match func(HistoryBuild) bool) (bool, error) {
	return slices.ContainsFunc(s.builds, match), nil
}

func (s *fakeHistoryStore) HasArtHistoryRecord(_ context.Context, id string) (bool, error) {
	return s.has(func(b HistoryBuild) bool { return b.RecordID == id })
}

func (s *fakeHistoryStore) HasArtHistoryNVR(_ context.Context, group, buildType, nvr string) (bool, error) {
	return s.has(func(b HistoryBuild) bool { return b.Group == group && b.Type == buildType && b.NVR == nvr })
}

func (s *fakeHistoryStore) HasArtHistoryDigest(_ context.Context, buildType, digest string) (bool, error) {
	return s.has(func(b HistoryBuild) bool { return b.Type == buildType && b.Digest == digest })
}

func (s *fakeHistoryStore) UpsertArtHistoryBuild(_ context.Context, b HistoryBuild) error {
	s.builds = append(s.builds, b)
	return nil
}

func (s *fakeHistoryStore) stored() []string {
	var got []string
	for _, b := range s.builds {
		got = append(got, b.Type+" "+b.NVR)
	}
	slices.Sort(got)
	return got
}

// quayART is a 3.17 stream: two FBCs, each holding a bundle whose operator
// and operands ART built, one operand external.
func quayART() *fakeART {
	qoB := successRecord(qoBundle)
	qoB.OperatorNVR, qoB.OperandNVRs = qoImage, []string{"external", clairImage}
	csoB := successRecord(csoBundle)
	csoB.OperatorNVR = csoImage
	qoF := successRecord(qoFBC)
	qoF.BundleNVRs = []string{qoBundle}
	csoF := successRecord(csoFBC)
	csoF.BundleNVRs = []string{csoBundle}
	f := &fakeART{records: []record{qoB, csoB, qoF, csoF, successRecord(qoImage), successRecord(clairImage), successRecord(csoImage)}}
	byName := func(nvr string) []searchBuild {
		rows := f.rows(func(r record) bool { return r.NVR == nvr })
		for i := range rows {
			rows[i].Name = nvr[:strings.Index(nvr, "-fbc-")+4]
		}
		return rows
	}
	f.days = map[string][]searchBuild{
		"^quay-operator-fbc$":               byName(qoFBC),
		"^container-security-operator-fbc$": byName(csoFBC),
	}
	return f
}

func syncWith(t *testing.T, f *fakeART) (*HistorySync, *fakeHistoryStore) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	store := &fakeHistoryStore{}
	h := NewHistorySync(NewClient(srv.URL, srv.Client()), store, slog.Default())
	h.pace.Reset(time.Millisecond)
	h.now = func() time.Time { return time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC) }
	return h, store
}

func sorted(s ...string) []string {
	slices.Sort(s)
	return s
}

// cappedDay lists searchCap rows for the testDay FBC listing: the first rows
// of each named listing, padded with pending rows of the first name.
func cappedDay(f *fakeART, names ...string) []searchBuild {
	var rows []searchBuild
	for _, n := range names {
		rows = append(rows, f.days["^"+n+"$"]...)
	}
	for len(rows) < searchCap {
		rows = append(rows, searchBuild{NVR: qoFBC, RecordID: fmt.Sprint("pad-", len(rows)), Name: names[0], Outcome: "pending"})
	}
	return rows
}

// The capped listing shows only quay-operator-fbc rows, so CSO's FBC is past
// the cut and only a split by the operators' known FBC names finds it.
func TestHistorySyncSplitsCappedDay(t *testing.T) {
	f := quayART()
	f.days["fbc$"] = cappedDay(f, "quay-operator-fbc")
	h, store := syncWith(t, f)
	h.SyncOnce(t.Context())

	want := sorted(
		"bundle "+csoBundle, "bundle "+qoBundle,
		"fbc "+csoFBC, "fbc "+qoFBC,
		"image "+csoImage, "image "+qoImage, "image "+clairImage,
	)
	if got := store.stored(); !slices.Equal(got, want) {
		t.Errorf("stored:\n got %q\nwant %q", got, want)
	}
	for _, b := range store.builds {
		if !strings.HasPrefix(b.RecordID, "ok-") {
			t.Errorf("stored %s record %s, want only successful records", b.NVR, b.RecordID)
		}
	}
	if !slices.Contains(store.scanned, "quay-3.17 fbc "+testDay) {
		t.Errorf("scanned %q, want %s marked", store.scanned, testDay)
	}
	var split []string
	for _, q := range f.searches {
		if q.Get("dateRange") == testDay+" to "+testDay {
			split = append(split, q.Get("name"))
		}
	}
	if want := []string{"fbc$", "^quay-operator-fbc$", "^container-security-operator-fbc$", "^quay-bridge-operator-fbc$"}; !slices.Equal(split, want) {
		t.Errorf("listings of %s: %q, want %q", testDay, split, want)
	}
}

func TestHistorySyncUnsplittableDay(t *testing.T) {
	f := quayART()
	f.days["fbc$"] = cappedDay(f, "quay-operator-fbc")
	f.days["^quay-operator-fbc$"] = f.days["fbc$"]
	h, store := syncWith(t, f)
	err := h.scanDay(t.Context(), "quay-3.17", testDay)
	if err == nil || !strings.Contains(err.Error(), "search cap") {
		t.Errorf("scan error %v, want the search cap", err)
	}
	h.SyncOnce(t.Context())
	if slices.Contains(store.scanned, "quay-3.17 fbc "+testDay) {
		t.Errorf("scanned %q, want %s not marked", store.scanned, testDay)
	}
	if len(store.builds) != 0 {
		t.Errorf("stored %q from a capped listing, want none", store.stored())
	}
}

func TestHistorySyncSkipsPending(t *testing.T) {
	f := quayART()
	// A day listing only a build still pending stores nothing.
	f.days["fbc$"] = []searchBuild{{NVR: qoFBC, RecordID: "pending-" + qoFBC, Name: "quay-operator-fbc", Outcome: "pending"}}
	h, store := syncWith(t, f)
	h.SyncOnce(t.Context())
	if len(store.builds) != 0 {
		t.Errorf("stored %q, want none", store.stored())
	}
	// Once it finished, the success row is stored and its pending twin is not.
	f.days["fbc$"] = f.days["^quay-operator-fbc$"]
	h.SyncOnce(t.Context())
	if got, want := store.stored(), sorted("bundle "+qoBundle, "fbc "+qoFBC, "image "+qoImage, "image "+clairImage); !slices.Equal(got, want) {
		t.Errorf("stored:\n got %q\nwant %q", got, want)
	}
	for _, b := range store.builds {
		if !strings.HasPrefix(b.RecordID, "ok-") {
			t.Errorf("stored %s record %s, want only successful records", b.NVR, b.RecordID)
		}
	}
}

func TestCloseBundleByDigest(t *testing.T) {
	f := quayART()
	h, store := syncWith(t, f)
	digest := strings.TrimPrefix(pullspec(qoBundle), "quay.io/redhat-user-workloads/ocp-art-tenant/art-images@")
	if err := h.CloseBundle(t.Context(), "quay-3.17", digest); err != nil {
		t.Fatal(err)
	}
	if got, want := store.stored(), sorted("bundle "+qoBundle, "image "+qoImage, "image "+clairImage); !slices.Equal(got, want) {
		t.Errorf("stored:\n got %q\nwant %q", got, want)
	}
	b := store.builds[len(store.builds)-1]
	if b.Digest != digest {
		t.Errorf("bundle %+v", b)
	}
	// A stored bundle is closed, so another call asks nothing.
	n := len(f.searches)
	if err := h.CloseBundle(t.Context(), "quay-3.17", digest); err != nil {
		t.Fatal(err)
	}
	if len(f.searches) != n {
		t.Errorf("searched %v for a stored bundle", f.searches[n:])
	}
}

func TestUnscannedDays(t *testing.T) {
	today := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	days := func(from, to int) []string {
		var out []string
		for i := from; i <= to; i++ {
			out = append(out, today.AddDate(0, 0, -i).Format(time.DateOnly))
		}
		return out
	}
	for _, tc := range []struct {
		name    string
		scanned []string
		want    int
	}{
		{"all scanned, today not", days(1, 119), 0},
		{"none scanned", nil, 119},
		{"oldest missing", days(0, 118), 1},
	} {
		if got := UnscannedDays(today, tc.scanned); got != tc.want {
			t.Errorf("%s: %d unscanned, want %d", tc.name, got, tc.want)
		}
	}
}
