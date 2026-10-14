package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/quay/release-readiness/internal/jira"
)

// fakeJira is one issue, PROJQUAY-1, whose writes are counted and applied.
type fakeJira struct {
	issue         jira.WriteIssue
	versions      map[string]jira.VersionField
	comments      []jira.Comment
	transitions   []jira.Transition
	transitionErr error
	commentErr    error
	writes        []string
}

func newFakeJira(status string, targets ...string) *fakeJira {
	return &fakeJira{
		issue:       jira.WriteIssue{Project: "PROJQUAY", Status: status, TargetVersions: targets},
		versions:    map[string]jira.VersionField{"quay-v3.18.2": {Name: "quay-v3.18.2"}},
		transitions: []jira.Transition{{ID: "11", To: "ON_QA"}, {ID: "31", To: releasePending}},
	}
}

func (f *fakeJira) GetIssue(context.Context, string) (jira.WriteIssue, error) {
	i := f.issue
	i.TargetVersions = slices.Clone(i.TargetVersions)
	return i, nil
}

func (f *fakeJira) ListVersions(context.Context) (map[string]jira.VersionField, error) {
	return f.versions, nil
}

func (f *fakeJira) TargetVersionEditable(context.Context, string) (bool, error) {
	return true, nil
}

func (f *fakeJira) GetTransitions(context.Context, string) ([]jira.Transition, error) {
	return f.transitions, nil
}

func (f *fakeJira) EditTargetVersions(_ context.Context, key, add, remove string) error {
	f.writes = append(f.writes, "edit "+key+" +"+add+" -"+remove)
	f.issue.TargetVersions = append(slices.DeleteFunc(f.issue.TargetVersions, func(n string) bool { return n == remove }), add)
	return nil
}

func (f *fakeJira) Transition(_ context.Context, key, id string) error {
	f.writes = append(f.writes, "transition "+key+" "+id)
	if f.transitionErr != nil {
		return f.transitionErr
	}
	f.issue.Status = releasePending
	return nil
}

func (f *fakeJira) ListComments(context.Context, string) ([]jira.Comment, error) {
	return f.comments, nil
}

func (f *fakeJira) AddComment(_ context.Context, key, body string) error {
	f.writes = append(f.writes, "comment "+key)
	f.comments = append(f.comments, jira.Comment{Body: body})
	return f.commentErr
}

var writeKey = ActionKey{FBCDigest: "sha256:f3", IssueKey: "PROJQUAY-1", Release: "r3"}

// newWriter returns a writer of j over a store holding one proposed action
// for PROJQUAY-1.
func newWriter(j *fakeJira) (*Writer, *fakeStore) {
	store := newFakeStore()
	store.actions[writeKey] = Action{
		Key:                  writeKey,
		Assembly:             "3.18.2",
		FBCNVR:               "quay-operator-fbc-3.18.2-r3.el9.ocp4.16",
		EvidenceJSON:         `[{"component":"quay-quay","repo":"https://github.com/quay/quay","sha":"` + sha(3) + `"}]`,
		DesiredTargetVersion: "quay-v3.18.2",
		Status:               StatusProposed,
	}
	return NewWriter(j, store, "PROJQUAY", slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func applyPending(t *testing.T, w *Writer) {
	t.Helper()
	if err := w.ApplyPending(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWriterDisabled(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.run(t)
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(100))
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-1 fix"}}
	j := newFakeJira("Verified", "quay-v3.18.z")
	fx.run(t)
	if len(j.writes) != 0 {
		t.Errorf("writer off: writes %v; want none", j.writes)
	}

	fx.sync.Writer = NewWriter(j, fx.store, "PROJQUAY", slog.New(slog.NewTextHandler(io.Discard, nil)))
	fx.run(t)
	if len(j.writes) != 3 {
		t.Errorf("writer on: writes %v; want edit, transition and comment", j.writes)
	}
}

func TestWriterAlreadyReleasePending(t *testing.T) {
	j := newFakeJira(releasePending, "quay-v3.18.z")
	w, store := newWriter(j)
	applyPending(t, w)
	if want := []string{"edit PROJQUAY-1 +quay-v3.18.2 -quay-v3.18.z", "comment PROJQUAY-1"}; !slices.Equal(j.writes, want) {
		t.Errorf("writes %v; want %v", j.writes, want)
	}
	a := store.actions[writeKey]
	if a.Status != StatusApplied || a.VersionStep != "edited" || a.TransitionStep != "already" || a.CommentStep != "added" || a.Attempts != 1 {
		t.Errorf("action %+v; want applied without a transition", a)
	}
	body := j.comments[0].Body
	for _, s := range []string{a.FBCNVR, "sha256:f3", "https://github.com/quay/quay/commit/" + sha(3), "release-readiness FBC stamp sha256:f3 PROJQUAY-1"} {
		if !strings.Contains(body, s) {
			t.Errorf("comment %q lacks %q", body, s)
		}
	}
}

func TestWriterVersionMissing(t *testing.T) {
	j := newFakeJira("Verified", "quay-v3.18.z")
	j.versions = map[string]jira.VersionField{}
	w, store := newWriter(j)
	applyPending(t, w)
	if a := store.actions[writeKey]; len(j.writes) != 0 || a.Status != StatusSkipped || a.SkipReason != "target version quay-v3.18.2 missing" {
		t.Errorf("writes %v, action %+v; want none and skipped", j.writes, a)
	}
}

func TestWriterMarkerPresent(t *testing.T) {
	j := newFakeJira(releasePending, "quay-v3.18.2")
	j.comments = []jira.Comment{{Body: "Included.\n\nrelease-readiness FBC stamp sha256:f3 PROJQUAY-1\n"}}
	w, store := newWriter(j)
	applyPending(t, w)
	if a := store.actions[writeKey]; len(j.writes) != 0 || a.Status != StatusApplied || a.CommentStep != "already" || a.VersionStep != "already" {
		t.Errorf("writes %v, action %+v; want none", j.writes, a)
	}
}

func TestWriterRetry(t *testing.T) {
	j := newFakeJira("Verified", "quay-v3.17.z", "quay-v3.18.z", "omr-v2.0.z")
	j.transitionErr = errors.New("JIRA API returned 500")
	w, store := newWriter(j)
	applyPending(t, w)
	a := store.actions[writeKey]
	if a.Status != StatusError || a.Error != "JIRA API returned 500" || a.VersionStep != "edited" || a.TransitionStep != "" || a.CommentStep != "" {
		t.Errorf("failed transition: action %+v; want error after the edit", a)
	}

	j.transitionErr = nil
	applyPending(t, w)
	want := []string{"edit PROJQUAY-1 +quay-v3.18.2 -quay-v3.18.z", "transition PROJQUAY-1 31", "transition PROJQUAY-1 31", "comment PROJQUAY-1"}
	if !slices.Equal(j.writes, want) {
		t.Errorf("writes %v; want %v", j.writes, want)
	}
	a = store.actions[writeKey]
	if a.Status != StatusApplied || a.Error != "" || a.TransitionStep != "transitioned" || a.Attempts != 2 {
		t.Errorf("retry: action %+v; want applied on attempt 2", a)
	}
}

// TestWriterLostReply is a comment that posts but whose reply is lost: the
// marker keeps the retry from posting it twice.
func TestWriterLostReply(t *testing.T) {
	j := newFakeJira(releasePending, "quay-v3.18.2")
	j.commentErr = errors.New("timeout")
	w, store := newWriter(j)
	applyPending(t, w)
	j.commentErr = nil
	applyPending(t, w)
	if a := store.actions[writeKey]; len(j.comments) != 1 || a.Status != StatusApplied || a.CommentStep != "already" || a.Attempts != 2 {
		t.Errorf("comments %d, action %+v; want one comment, applied on attempt 2", len(j.comments), a)
	}
}

func TestWriterGuards(t *testing.T) {
	for name, tc := range map[string]struct {
		jira   func(*fakeJira)
		writes []string
		check  func(Action) bool
	}{
		"other project": {
			jira: func(j *fakeJira) { j.issue.Project = "OTHER" },
			check: func(a Action) bool {
				return a.Status == StatusSkipped && a.SkipReason == "project OTHER is not PROJQUAY"
			},
		},
		"other stream": {
			jira: func(j *fakeJira) { j.issue.TargetVersions = []string{"quay-v3.17.z"} },
			check: func(a Action) bool {
				return a.Status == StatusSkipped && a.SkipReason == "Target Version [quay-v3.17.z] has neither quay-v3.18.z nor quay-v3.18.2"
			},
		},
		"not verified": {
			jira:   func(j *fakeJira) { j.issue.Status = "In Progress" },
			writes: []string{"edit PROJQUAY-1 +quay-v3.18.2 -quay-v3.18.z", "comment PROJQUAY-1"},
			check:  func(a Action) bool { return a.Status == StatusApplied && a.TransitionStep == "none from In Progress" },
		},
		"release pending not offered": {
			jira:   func(j *fakeJira) { j.transitions = j.transitions[:1] },
			writes: []string{"edit PROJQUAY-1 +quay-v3.18.2 -quay-v3.18.z", "comment PROJQUAY-1"},
			check: func(a Action) bool {
				return a.Status == StatusApplied && a.TransitionStep == "none offered from Verified"
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			j := newFakeJira("Verified", "quay-v3.18.z")
			tc.jira(j)
			w, store := newWriter(j)
			applyPending(t, w)
			if a := store.actions[writeKey]; !slices.Equal(j.writes, tc.writes) || !tc.check(a) {
				t.Errorf("writes %v, action %+v; want writes %v", j.writes, a, tc.writes)
			}
		})
	}
}
