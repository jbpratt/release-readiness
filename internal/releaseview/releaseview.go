// Package releaseview derives the component set shown for a release from the
// Konflux applications that make it up.
package releaseview

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

const baseImagesApp = "quay-images-base"

var quayApp = regexp.MustCompile(`^quay-\d+-\d+$`)

// Component is one component image from a snapshot, with its provenance.
type Component struct {
	Name, Image, GitSHA, GitURL, Application, Snapshot string
	CreatedAt                                          time.Time
	SnapshotID, RowID                                  int64
}

// Applications returns the Konflux applications that make up a release.
// FBC is the shipped product; quay-X-Y and the quay-X-Y-* base images from
// quay-images-base are its components. fbc-quay-X-Y only exists for 3.16+, so
// older releases simply have no rows for it.
func Applications(konfluxApp string) []string {
	if konfluxApp == "" {
		return nil
	}
	if quayApp.MatchString(konfluxApp) {
		return []string{konfluxApp, "fbc-" + konfluxApp, baseImagesApp}
	}
	return []string{konfluxApp}
}

// Select returns the newest row per component name among the release's
// applications, sorted by name.
func Select(konfluxApp string, rows []Component) []Component {
	apps := Applications(konfluxApp)
	newest := map[string]Component{}
	for _, r := range rows {
		if !slices.Contains(apps, r.Application) {
			continue
		}
		if r.Application == baseImagesApp && !strings.HasPrefix(r.Name, konfluxApp+"-") {
			continue
		}
		if cur, ok := newest[r.Name]; !ok || newer(r, cur) {
			newest[r.Name] = r
		}
	}
	out := make([]Component, 0, len(newest))
	for _, c := range newest {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Component) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func newer(a, b Component) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	if a.SnapshotID != b.SnapshotID {
		return a.SnapshotID > b.SnapshotID
	}
	return a.RowID > b.RowID
}
