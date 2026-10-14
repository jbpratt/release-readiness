package server

import (
	"slices"
	"testing"

	"github.com/quay/release-readiness/internal/model"
)

func TestSetVerdict(t *testing.T) {
	// clean is a staged 3.17.6 candidate with nothing wrong: each operator on
	// one bundle, matching the image shipment.
	clean := func() *model.ReleaseCandidate {
		f := &model.FBCCandidate{Source: "stage_snapshots", Pullspecs: []string{"fbc@sha256:1"}}
		for _, op := range []string{"quay-operator", "container-security-operator", "quay-bridge-operator"} {
			f.Operators = append(f.Operators, model.OperatorFBC{
				Operator: op, HeadBundleNVR: op + "-bundle", HeadCSV: op + ".v3.17.6",
				Members:    []model.FBCMember{{OCP: "4.20", HeadBundleNVR: op + "-bundle"}},
				MissingOCP: []string{},
				ImageCheck: model.ImageCheck{State: "matches", BundleNVR: op + "-bundle"},
			})
		}
		return &model.ReleaseCandidate{FBCCandidate: f}
	}
	newer := func(changes ...string) []model.ImageCompare {
		c := model.ImageCompare{Kind: "newer", Label: "quay-operator-bundle-new"}
		for _, ch := range changes {
			c.Rows = append(c.Rows, model.ImageCompareRow{Change: ch})
		}
		return []model.ImageCompare{c}
	}
	for _, tc := range []struct {
		name string
		edit func(r *model.ReleaseCandidate)
		// pending counts the candidate's unread FBCs.
		pending int
		state   string
		items   []string
	}{
		{name: "ready", edit: func(*model.ReleaseCandidate) {}, state: "ready"},
		{name: "checking", pending: 1, state: "checking", edit: func(r *model.ReleaseCandidate) {
			r.Reading.Pending = 1
			r.FBCCandidate.Operators[0].ImageCheck.State = "differs"
		}},
		// Staging needs no read: it blocks while the candidate's are pending.
		{name: "checking, missing_operator", pending: 1, state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.Reading.Pending = 1
			r.FBCCandidate.Operators[0].ImageCheck.State = "differs"
			o := &r.FBCCandidate.Operators[2]
			o.HeadBundleNVR, o.Members = "", nil
		}, items: []string{"blocker quay-bridge-operator Quay (release-from-fbc) missing_operator: quay-bridge-operator is not in the stage shipment"}},
		// Like 3.17.6: the stage shipment is read and the stream is not, so
		// only the stream's newer build waits.
		{name: "stream pending", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.Reading.Pending = 3
			r.FBCCandidate.Operators[0].Compare = newer("new source")
			o := &r.FBCCandidate.Operators[1]
			o.HeadBundleNVR, o.Members = "", nil
		}, items: []string{"blocker container-security-operator Quay (release-from-fbc) missing_operator: container-security-operator is not in the stage shipment"}},
		// Until the backfill has read every day, a slot's newest FBC may be
		// unseen.
		{name: "history incomplete", state: "checking", edit: func(r *model.ReleaseCandidate) {
			r.Reading.HistoryDays = 5
			r.FBCCandidate.Operators[0].ImageCheck.State = "differs"
			r.FBCCandidate.Operators[0].Compare = newer("new source")
		}},
		{name: "shipped", state: "shipped", edit: func(r *model.ReleaseCandidate) {
			r.Shipped, r.Reading.Pending = true, 1
			r.FBCCandidate.Operators[0].MissingOCP = []string{"4.21"}
			r.FBCCandidate.Operators[0].ImageCheck.State = "differs"
		}},
		{name: "missing_operator", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			o := &r.FBCCandidate.Operators[2]
			o.HeadBundleNVR, o.HeadCSV, o.Members, o.MissingOCP = "", "", nil, []string{"4.20"}
			o.ImageCheck.State = "absent"
		}, items: []string{"blocker quay-bridge-operator Quay (release-from-fbc) missing_operator: quay-bridge-operator is not in the stage shipment"}},
		// A stream candidate is not up for release, whatever its operators.
		{name: "not_staged", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Source = "stream"
			r.FBCCandidate.Operators[0].ImageCheck = model.ImageCheck{State: "differs", BundleNVR: "image-bundle"}
		}, items: []string{
			"blocker  Quay (release-from-fbc) not_staged: Not in a stage shipment",
			"blocker quay-operator ART image_mismatch: Image shipment has bundle image-bundle, the FBCs ship quay-operator-bundle",
		}},
		{name: "csv_mismatch", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[0].HeadCSV = "quay-operator.v3.99.0-dev"
		}, items: []string{"blocker quay-operator Quay/ART csv_mismatch: quay-operator FBCs ship v3.99.0-dev, not v3.17.6"}},
		{name: "mixed_builds", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			o := &r.FBCCandidate.Operators[1]
			o.Conflicts = []model.FBCMember{{OCP: "4.21", HeadBundleNVR: "cso-old"}, {OCP: "4.22", HeadBundleNVR: "cso-old"}}
		}, items: []string{"blocker container-security-operator ART mixed_builds: container-security-operator FBCs ship different bundles: OCP 4.21, 4.22 not on container-security-operator-bundle"}},
		{name: "image_mismatch", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[0].ImageCheck = model.ImageCheck{State: "differs", BundleNVR: "image-bundle"}
		}, items: []string{"blocker quay-operator ART image_mismatch: Image shipment has bundle image-bundle, the FBCs ship quay-operator-bundle"}},
		{name: "read_failed", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			o := &r.FBCCandidate.Operators[2]
			o.Conflicts = []model.FBCMember{{OCP: "5.0", ReadError: "no channel stable-5.0"}}
		}, items: []string{"blocker quay-bridge-operator Quay/ART read_failed: quay-bridge-operator OCP 5.0: can't read FBC: no channel stable-5.0"}},
		// Like CSO 5.0: its FBC never reached stage.
		{name: "stage_release_failed", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[1].Members[0].StageFailed = &model.KonfluxRelease{ReleasedReason: "Failed", FailedTask: "verify-conforma"}
		}, items: []string{"blocker container-security-operator ART/Konflux stage_release_failed: container-security-operator OCP 4.20: stage release failed (verify-conforma)"}},
		{name: "stage_push_failed", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[0].FBCRIStage = model.CIState{State: "failed", Release: &model.KonfluxRelease{FailedTask: "push-snapshot"}}
		}, items: []string{"blocker quay-operator ART/Konflux stage_push_failed: quay-operator bundle quay-operator-bundle: image push to stage failed (push-snapshot)"}},
		// Built for an OCP version and staged without it, nothing releases.
		{name: "missing_ocp", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Pullspecs = []string{}
			r.FBCCandidate.Operators[2].MissingOCP = []string{"4.21"}
		}, items: []string{"blocker quay-bridge-operator ART missing_ocp: quay-bridge-operator OCP 4.21 not in the stage shipment"}},
		{name: "unknown", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Pullspecs = []string{}
			r.FBCCandidate.Operators[1].Conflicts = []model.FBCMember{{OCP: "4.21"}}
			r.FBCCandidate.Unknown = []model.FBCMember{{Digest: "sha256:2"}}
		}, items: []string{
			"blocker container-security-operator  no_head_bundle: container-security-operator OCP 4.21: FBC heads no known bundle",
			"blocker   unknown_fbc: Staged FBCs with no ART build name: 1",
		}},
		{name: "stream ahead, rebuild only", state: "ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[0].Compare = newer("same", "rebuild", "rebuild")
		}, items: []string{"info quay-operator  stream_ahead: quay-operator: newer build not on stage, 2 images changed"}},
		// Blockers come first, whatever their operator.
		{name: "stream ahead, new source", state: "not_ready", edit: func(r *model.ReleaseCandidate) {
			r.FBCCandidate.Operators[0].Compare = newer("same", "rebuild", "new source", "new build")
			r.FBCCandidate.Operators[1].ImageCheck = model.ImageCheck{State: "differs", BundleNVR: "image-bundle"}
		}, items: []string{
			"blocker container-security-operator ART image_mismatch: Image shipment has bundle image-bundle, the FBCs ship container-security-operator-bundle",
			"warning quay-operator  stream_ahead: quay-operator: newer build not on stage, 3 images changed",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := clean()
			tc.edit(r)
			setVerdict(r, "3.17.6", tc.pending)
			var items []string
			for _, it := range r.Verdict.Items {
				items = append(items, it.Level+" "+it.Operator+" "+it.Owner+" "+it.Code+": "+it.Text)
			}
			if r.Verdict.State != tc.state || !slices.Equal(items, tc.items) || r.Verdict.Items == nil {
				t.Errorf("verdict %s %q, want %s %q", r.Verdict.State, items, tc.state, tc.items)
			}
			// Record mode: a shipped candidate misses nothing and releases nothing.
			if f := r.FBCCandidate; r.Shipped && (len(f.Pullspecs) > 0 || len(f.Operators[0].MissingOCP) > 0) {
				t.Errorf("shipped: pullspecs %v, missing %v", f.Pullspecs, f.Operators[0].MissingOCP)
			}
		})
	}
}
