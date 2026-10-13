package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/model"
)

// handleGetReleaseCI serves the version's ART FBC builds, newest first, each
// with the periodic runs that installed from it.
func (s *Server) handleGetReleaseCI(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	builds, err := s.releaseCI(r.Context(), release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, builds)
}

func (s *Server) releaseCI(ctx context.Context, release *model.ReleaseVersion) ([]model.FBCBuild, error) {
	app := release.KonfluxApplication
	m := quayVersion.FindStringSubmatch(release.Name)
	if app == "" || m == nil {
		return []model.FBCBuild{}, nil
	}
	assembly := m[1]
	xy := assembly[:strings.LastIndex(assembly, ".")]
	builds, err := s.db.FBCHistory(ctx, "quay-"+xy, assembly, "fbc-"+app)
	if err != nil {
		return nil, err
	}
	c, err := s.releaseCandidate(ctx, release)
	if err != nil {
		return nil, err
	}
	candidate := fbcDigests(c.FBCCandidate)
	runs, err := s.db.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return nil, err
	}
	// runs is newest first.
	byDigest := map[string][]model.FBCRun{}
	for _, run := range runs {
		if d := imageDigest(run.CatalogRef); d != "" && run.StartedAt != nil {
			byDigest[d] = append(byDigest[d], model.FBCRun{
				Job: run.JobName, State: run.State, StartedAt: *run.StartedAt, ProwURL: run.ProwURL, SippyURL: sippyJobURL(xy, run.JobName),
			})
		}
	}
	for i := range builds {
		b := &builds[i]
		if b.State != "shipped" && slices.Contains(candidate, b.Digest) {
			b.State = "candidate"
		}
		b.Runs = byDigest[b.Digest]
		if b.Runs == nil {
			b.Runs = []model.FBCRun{}
		}
	}
	return builds, nil
}

// sippyJobURL returns Sippy's page of a periodic job of the quay-X.Y release.
func sippyJobURL(xy, job string) string {
	filters, _ := json.Marshal(map[string][]map[string]string{
		"items": {{"columnField": "name", "operatorValue": "equals", "value": job}},
	})
	return "https://sippy.dptools.openshift.org/sippy-ng/jobs/quay-" + xy + "?filters=" + url.QueryEscape(string(filters))
}
