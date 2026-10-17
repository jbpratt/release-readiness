package release

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
	"github.com/quay/release-readiness/internal/model"
)

// Shipment MRs are polled for unshipped quay-vX.Y.Z versions, archived ones
// included, and for any whose MR is still open, since a version can ship
// before ART merges its MR.
func TestShipmentVersions(t *testing.T) {
	ctx := t.Context()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":1,"data":[{"repositories":[{"published":true,"tags":[{"name":"v3.17.5"}]}]}]}`))
	}))
	defer cat.Close()
	shipped := catalog.NewShipped(catalog.NewClient(cat.URL, cat.Client()), slog.Default())
	shipped.Refresh(ctx)

	for _, rv := range []model.ReleaseVersion{
		{Name: "quay-v3.18.0", Released: true},
		{Name: "quay-v3.17.4", Released: true},
		{Name: "quay-v3.17.5"},
		{Name: "quay-v3.17.6"},
		{Name: "quay-v3.16.9", Archived: true},
		{Name: "omr-v1.2.3"},
		{Name: "quay-v3.18"},
	} {
		if err := database.UpsertReleaseVersion(ctx, &rv); err != nil {
			t.Fatalf("upsert release: %v", err)
		}
	}
	for _, mr := range []model.ShipmentMR{
		{Version: "quay-v3.18.0", IID: 1, State: "opened", Source: "pointer"},
		{Version: "quay-v3.17.4", IID: 2, State: "merged", Source: "pointer"},
	} {
		if err := database.UpsertShipmentMR(ctx, &mr); err != nil {
			t.Fatalf("upsert shipment MR: %v", err)
		}
	}

	polled, err := NewSelector(database, shipped).ShipmentVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"quay-v3.16.9", "quay-v3.17.6", "quay-v3.18.0"}; !slices.Equal(polled, want) {
		t.Errorf("polled %v, want %v", polled, want)
	}
}
