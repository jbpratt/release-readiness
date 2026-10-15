package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/quay/release-readiness/internal/artbuild"
	"github.com/quay/release-readiness/internal/artfbc"
	"github.com/quay/release-readiness/internal/model"
)

// fbcTags is a version's operator FBCs as the tags ART publishes them under,
// which release-from-fbc takes as fbc-pullspecs. Source is "candidate" when
// the slots looked up are the release candidate's, else "ocp-build-data" when
// they are every operator at each OCP version the stream targets. Pullspecs
// is empty while Errors is not.
type fbcTags struct {
	Repository string            `json:"repository"`
	Source     string            `json:"source"`
	Pullspecs  []string          `json:"pullspecs"`
	Tags       []artfbc.Resolved `json:"tags"`
	Errors     []string          `json:"errors"`
	Warnings   []string          `json:"warnings"`
}

// handleGetFBCTags serves the newest multi-arch tag of each of the version's
// operator FBC slots in ART's public repository, checked against the
// candidate's staged digest. Quay is read from here, cached for a short while.
func (s *Server) handleGetFBCTags(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	release, err := s.db.GetReleaseVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	if s.ArtFBC == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("the ART FBC tag lookup is off"))
		return
	}
	c, err := s.selectCandidate(r.Context(), release)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if c.assembly == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q is not a version ART builds an FBC of: %s", version, c.reason))
		return
	}
	resp, _, err := s.candidateResponse(r.Context(), release, c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := fbcTags{Repository: artfbc.Repository, Source: "candidate"}
	slots := candidateSlots(resp.FBCCandidate)
	if len(slots) == 0 {
		out.Source = "ocp-build-data"
		ocps, err := s.ArtFBC.TargetVersions(r.Context(), c.assembly[:strings.LastIndex(c.assembly, ".")])
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		for _, o := range artbuild.Operators {
			for _, ocp := range ocps {
				slots = append(slots, artfbc.Slot{Operator: o, OCP: ocp})
			}
		}
	}
	newest, err := s.newestFBCTags(r.Context(), c.assembly, slots)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	res := artfbc.Resolve(slots, newest)
	out.Pullspecs, out.Tags, out.Errors, out.Warnings = res.Pullspecs, res.Tags, res.Errors, res.Warnings
	writeJSON(w, http.StatusOK, out)
}

// newestFBCTags returns each operator's newest tags of assembly by OCP
// version, for the OCP versions of slots, the operators read at once.
func (s *Server) newestFBCTags(ctx context.Context, assembly string, slots []artfbc.Slot) (map[string]map[string]artfbc.Tag, error) {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		newest = map[string]map[string]artfbc.Tag{}
		errs   []error
	)
	for _, o := range artbuild.Operators {
		wg.Go(func() {
			var ocps []string
			for _, sl := range slots {
				if sl.Operator == o {
					ocps = append(ocps, sl.OCP)
				}
			}
			tags, err := s.ArtFBC.Tags(ctx, o, assembly, ocps)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			newest[o] = artfbc.Newest(tags, o, assembly)
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, fmt.Errorf("read ART's FBC tags: %w", errs[0])
	}
	return newest, nil
}

// candidateSlots returns the operator FBC slots of c, by operator, then OCP
// version, with each slot's staged digest: the digest of the FBC the
// candidate holds for it, and its NVR. An operator with no slot of its own
// takes the OCP versions of the others, so that its absence is a missing tag.
// A nil c has none.
func candidateSlots(c *model.FBCCandidate) []artfbc.Slot {
	if c == nil {
		return nil
	}
	var all []string
	per := map[string][]artfbc.Slot{}
	for _, o := range c.Operators {
		held := map[string]model.FBCMember{}
		for _, m := range slices.Concat(o.Members, o.Conflicts) {
			held[m.OCP] = m
		}
		ocps := slices.Clone(o.MissingOCP)
		for ocp := range held {
			ocps = append(ocps, ocp)
		}
		for _, ocp := range ocps {
			m := held[ocp]
			per[o.Operator] = append(per[o.Operator], artfbc.Slot{Operator: o.Operator, OCP: ocp, Staged: m.Digest, StagedNVR: m.NVR})
			if !slices.Contains(all, ocp) {
				all = append(all, ocp)
			}
		}
	}
	slices.SortFunc(all, compareVersions)
	var slots []artfbc.Slot
	for _, o := range c.Operators {
		ss := per[o.Operator]
		if len(ss) == 0 {
			for _, ocp := range all {
				ss = append(ss, artfbc.Slot{Operator: o.Operator, OCP: ocp})
			}
		}
		slices.SortFunc(ss, func(a, b artfbc.Slot) int { return compareVersions(a.OCP, b.OCP) })
		slots = append(slots, ss...)
	}
	return slots
}
