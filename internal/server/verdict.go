package server

import (
	"fmt"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/model"
)

// setVerdict sets resp's verdict on its FBC candidate for version assembly.
// Nothing blocks or warns while FBC reads are pending: an unread FBC is RR's
// state, not the release's. A shipped version's candidate is the record of
// what shipped, so it misses no OCP version and has nothing to release.
func setVerdict(resp *model.ReleaseCandidate, assembly string) {
	v := &resp.Verdict
	v.Items = []model.VerdictItem{}
	f := resp.FBCCandidate
	switch {
	case resp.Shipped:
		v.State = "shipped"
		if f != nil {
			f.Pullspecs = []string{}
			for i := range f.Operators {
				f.Operators[i].MissingOCP = []string{}
			}
		}
		return
	case resp.Reading.Pending > 0:
		v.State = "checking"
		return
	case f == nil:
		v.State = "not_ready"
		return
	}
	add := func(level, operator, owner, code, text string) {
		v.Items = append(v.Items, model.VerdictItem{Level: level, Operator: operator, Owner: owner, Code: code, Text: text})
	}
	staged := f.Source == "stage_snapshots"
	if !staged {
		add("blocker", "", "ART", "not_staged", "Not in a stage shipment")
	}
	for _, o := range f.Operators {
		op := o.Operator
		if staged && len(o.Members)+len(o.Conflicts) == 0 {
			add("blocker", op, "ART", "missing_operator", op+" is not in the stage shipment")
		} else if staged && len(o.MissingOCP) > 0 {
			add("blocker", op, "ART", "missing_ocp", fmt.Sprintf("%s OCP %s not in the stage shipment", op, strings.Join(o.MissingOCP, ", ")))
		}
		if _, csv, ok := strings.Cut(o.HeadCSV, ".v"); ok && csv != assembly {
			add("blocker", op, "Quay/ART", "csv_mismatch", fmt.Sprintf("%s FBCs ship v%s, not v%s", op, csv, assembly))
		}
		var other []string
		for _, m := range o.Conflicts {
			if m.HeadBundleNVR != "" {
				other = append(other, m.OCP)
			}
		}
		if len(other) > 0 {
			add("blocker", op, "ART", "mixed_builds", fmt.Sprintf("%s FBCs ship different bundles: OCP %s not on %s", op, strings.Join(other, ", "), o.HeadBundleNVR))
		}
		if o.ImageCheck.State == "differs" {
			add("blocker", op, "ART", "image_mismatch", fmt.Sprintf("Image shipment has bundle %s, the FBCs ship %s", o.ImageCheck.BundleNVR, o.HeadBundleNVR))
		}
		for _, m := range slices.Concat(o.Members, o.Conflicts) {
			switch {
			case !staged:
			case m.ReadError != "":
				add("blocker", op, "", "read_failed", fmt.Sprintf("%s OCP %s: can't read FBC: %s", op, m.OCP, m.ReadError))
			case m.HeadBundleNVR == "":
				add("blocker", op, "", "no_head_bundle", fmt.Sprintf("%s OCP %s: FBC heads no known bundle", op, m.OCP))
			}
		}
		for _, c := range o.Compare {
			if c.Kind != "newer" {
				continue
			}
			level, changed := "info", 0
			for _, row := range c.Rows {
				if row.Change != "same" {
					changed++
				}
				// A rebuild from the same source is info; anything else, an
				// unknown commit included, may change behaviour.
				if row.Change != "same" && row.Change != "rebuild" {
					level = "warning"
				}
			}
			add(level, op, "", "stream_ahead", fmt.Sprintf("%s: newer build not on stage, %d images changed", op, changed))
		}
	}
	if staged && len(f.Unknown) > 0 {
		add("blocker", "", "", "unknown_fbc", fmt.Sprintf("Staged FBCs with no ART build name: %d", len(f.Unknown)))
	}
	rank := map[string]int{"blocker": 0, "warning": 1, "info": 2}
	slices.SortStableFunc(v.Items, func(a, b model.VerdictItem) int { return rank[a.Level] - rank[b.Level] })
	v.State = "ready"
	if len(v.Items) > 0 && v.Items[0].Level == "blocker" {
		v.State = "not_ready"
	}
}
