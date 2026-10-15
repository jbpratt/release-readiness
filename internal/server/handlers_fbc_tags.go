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
// they are every operator at each OCP version the stream targets. Each tag is
// the one pointing at the FBC RR staged for its slot (basis "staged"), and
// the newest tag only for a slot RR staged none of (basis "newest", a
// warning); Info names the slots ART has built again since. Pullspecs is
// empty while Errors is not.
type fbcTags struct {
	Repository string            `json:"repository"`
	Source     string            `json:"source"`
	Pullspecs  []string          `json:"pullspecs"`
	Tags       []artfbc.Resolved `json:"tags"`
	Errors     []string          `json:"errors"`
	Warnings   []string          `json:"warnings"`
	Info       []string          `json:"info"`
}

// handleGetFBCTags serves the multi-arch tag of each of the version's
// operator FBC slots in ART's public repository that points at the digest RR
// staged for the slot, else, for a slot with none staged, its newest tag.
// Quay is read from here, cached for a short while.
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
	tags, err := s.fbcTagsOf(r.Context(), c.assembly, slots)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	res := artfbc.Resolve(slots, c.assembly, tags)
	out.Pullspecs, out.Tags, out.Errors, out.Warnings, out.Info = res.Pullspecs, res.Tags, res.Errors, res.Warnings, res.Info
	writeJSON(w, http.StatusOK, out)
}

// fbcTagsOf returns each operator's tags of assembly that answer slots, the
// operators read at once.
func (s *Server) fbcTagsOf(ctx context.Context, assembly string, slots []artfbc.Slot) (map[string][]artfbc.Tag, error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		tags = map[string][]artfbc.Tag{}
		errs []error
	)
	for _, o := range artbuild.Operators {
		wg.Go(func() {
			own := slices.DeleteFunc(slices.Clone(slots), func(sl artfbc.Slot) bool { return sl.Operator != o })
			got, err := s.ArtFBC.Tags(ctx, o, assembly, own)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			tags[o] = got
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, fmt.Errorf("read ART's FBC tags: %w", errs[0])
	}
	return tags, nil
}

// candidateSlots returns the operator FBC slots of c, by operator, then OCP
// version, with each slot's staged digest: the digest of the FBC the
// candidate holds for it, and its NVR, only when c is what ART staged in the
// FBC Snapshots, not what its stream built. An operator with no slot of its
// own takes the OCP versions of the others, so that its absence is a missing
// tag. A nil c has none.
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
			if c.Source != "stage_snapshots" {
				m = model.FBCMember{}
			}
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
