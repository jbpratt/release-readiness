package releaseview

import (
	"slices"
	"testing"
	"time"
)

func TestApplications(t *testing.T) {
	tests := []struct {
		app  string
		want []string
	}{
		{"quay-3-18", []string{"quay-3-18", "fbc-quay-3-18", "quay-images-base"}},
		{"omr-2-0", []string{"omr-2-0"}},
		{"", nil},
	}
	for _, tt := range tests {
		if got := Applications(tt.app); !slices.Equal(got, tt.want) {
			t.Errorf("Applications(%q) = %v, want %v", tt.app, got, tt.want)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		app, application string
		components       []string
		want             bool
	}{
		{"quay-3-18", "fbc-quay-3-18", nil, true},
		{"quay-3-18", "quay-images-base", []string{"quay-3-9-base-rhel9", "quay-3-18-base-rhel9"}, true},
		{"quay-3-18", "quay-images-base", []string{"quay-3-9-base-rhel9"}, false},
		{"quay-3-18", "quay-3-17", nil, false},
	}
	for _, tt := range tests {
		if got := Contains(tt.app, tt.application, tt.components); got != tt.want {
			t.Errorf("Contains(%q, %q, %v) = %v, want %v", tt.app, tt.application, tt.components, got, tt.want)
		}
	}
}

func TestSelect(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	row := func(id int64, app, name string, created time.Time, snapID int64) Component {
		return Component{Name: name, Application: app, CreatedAt: created, SnapshotID: snapID, RowID: id}
	}
	rows := []Component{
		row(1, "quay-3-18", "quay-3-18-quay", t0, 10),
		row(2, "fbc-quay-3-18", "fbc-quay-3-18-a", t0, 20),
		row(3, "fbc-quay-3-18", "fbc-quay-3-18-b", t1, 21),
		row(4, "quay-images-base", "quay-3-18-base-rhel9", t1, 30),
		row(5, "quay-3-18", "quay-3-18-base-rhel9", t0, 11),
		row(6, "quay-images-base", "quay-3-9-base-rhel9", t1, 30),
		row(7, "quay-3-17", "quay-3-17-quay", t1, 40),
		// Same CreatedAt: higher SnapshotID wins.
		row(8, "quay-3-18", "quay-3-18-clair", t0, 12),
		row(9, "quay-3-18", "quay-3-18-clair", t0, 13),
		// Same CreatedAt and SnapshotID: higher RowID wins.
		row(11, "quay-3-18", "quay-3-18-builder", t0, 14),
		row(10, "quay-3-18", "quay-3-18-builder", t0, 14),
		row(12, "quay-3-14", "quay-3-14-quay", t0, 50),
		row(13, "quay-images-base", "quay-3-14-base-rhel8", t0, 51),
	}

	tests := []struct {
		app  string
		want []int64 // RowIDs, in name order
	}{
		{"quay-3-18", []int64{2, 3, 4, 11, 9, 1}},
		{"quay-3-14", []int64{13, 12}},
		{"quay-3-17", []int64{7}},
		{"", nil},
	}
	for _, tt := range tests {
		var got []int64
		for _, c := range Select(tt.app, rows) {
			got = append(got, c.RowID)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Select(%q) row IDs = %v, want %v", tt.app, got, tt.want)
		}
	}
}
