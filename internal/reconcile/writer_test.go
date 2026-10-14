package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/jira"
)

// fakeJira is one issue, PROJQUAY-1, whose writes are counted and applied.
type fakeJira struct {
	issue         jira.WriteIssue
	versions      map[string]jira.VersionField
	comments      []jira.Comment
	transitions   []jira.Transition
	transitionErr error
	issueErr      error
	reads         int
	writes        []string
}

func newFakeJira(status string, targets ...string) *fakeJira {
	return &fakeJira{
		issue:       jira.WriteIssue{Key: "PROJQUAY-1", Project: "PROJQUAY", Type: "Bug", Status: status, TargetVersions: targets},
		versions:    map[string]jira.VersionField{"quay-v3.18.2": {Name: "quay-v3.18.2"}},
		transitions: []jira.Transition{{ID: "11", Name: "Back", To: "ON_QA"}, {ID: "31", Name: "Release", To: releasePending}},
	}
}

func (f *fakeJira) GetIssue(context.Context, string) (jira.WriteIssue, error) {
	f.reads++
	i := f.issue
	i.TargetVersions = slices.Clone(i.TargetVersions)
	return i, f.issueErr
}

func (f *fakeJira) ListVersions(context.Context) (map[string]jira.VersionField, error) {
	return f.versions, nil
}

func (f *fakeJira) GetEditMeta(context.Context, string) (jira.EditMeta, error) {
	return jira.EditMeta{TargetVersion: true}, nil
}

func (f *fakeJira) GetTransitions(context.Context, string) ([]jira.Transition, error) {
	return f.transitions, nil
}

func (f *fakeJira) EditTargetVersions(_ context.Context, key string, names []string) error {
	f.writes = append(f.writes, "edit "+key+" "+strings.Join(names, ","))
	f.issue.TargetVersions = names
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

func (f *fakeJira) AddComment(_ context.Context, key, body string) (string, error) {
	f.writes = append(f.writes, "comment "+key)
	f.comments = append(f.comments, jira.Comment{ID: "c1", Body: body})
	return "c1", nil
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
	w := NewWriter(j, store, "PROJQUAY", slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
	return w, store
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

func TestWriterDryRun(t *testing.T) {
	j := newFakeJira("Verified", "quay-v3.18.z")
	w, store := newWriter(j)
	before := store.actions[writeKey]
	w.DryRun = true
	// A failed read is retried on the next pass.
	j.issueErr = errors.New("JIRA API returned 500")
	applyPending(t, w)
	j.issueErr = nil
	reads := j.reads
	applyPending(t, w)
	if j.reads == reads {
		t.Error("dry run after a failed read did not retry")
	}
	if len(j.writes) != 0 || !reflect.DeepEqual(store.actions[writeKey], before) {
		t.Errorf("dry run: writes %v, action %+v; want none and unchanged", j.writes, store.actions[writeKey])
	}
	reads = j.reads
	applyPending(t, w)
	if j.reads != reads {
		t.Errorf("second dry run read the issue %d more times; want none", j.reads-reads)
	}
}

func TestWriterKillFile(t *testing.T) {
	j := newFakeJira("Verified", "quay-v3.18.z")
	w, store := newWriter(j)
	w.KillFile = filepath.Join(t.TempDir(), "stop")
	if err := os.WriteFile(w.KillFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	applyPending(t, w)
	if a := store.actions[writeKey]; len(j.writes) != 0 || a.Status != StatusProposed {
		t.Errorf("kill file: writes %v, action %+v; want none and still proposed", j.writes, a)
	}
}

func TestWriterAlreadyReleasePending(t *testing.T) {
	j := newFakeJira(releasePending, "quay-v3.18.z")
	w, store := newWriter(j)
	applyPending(t, w)
	if want := []string{"edit PROJQUAY-1 quay-v3.18.2", "comment PROJQUAY-1"}; !slices.Equal(j.writes, want) {
		t.Errorf("writes %v; want %v", j.writes, want)
	}
	a := store.actions[writeKey]
	if a.Status != StatusApplied || a.VersionStep != "edited" || a.TransitionStep != "already" || a.CommentStep != "added" ||
		a.ActualTargetVersion != "quay-v3.18.2" || a.StatusBefore != releasePending || a.Attempts != 1 {
		t.Errorf("action %+v; want applied without a transition", a)
	}
	body := j.comments[0].Body
	for _, s := range []string{a.FBCNVR, "sha256:f3", "https://github.com/quay/quay/commit/" + sha(3), a.CommentMarker} {
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
	j.comments = []jira.Comment{{ID: "c9", Body: "Included.\n\nrelease-readiness FBC stamp sha256:f3 PROJQUAY-1\n"}}
	w, store := newWriter(j)
	applyPending(t, w)
	if a := store.actions[writeKey]; len(j.writes) != 0 || a.Status != StatusApplied || a.CommentID != "c9" || a.CommentStep != "already" || a.VersionStep != "already" {
		t.Errorf("writes %v, action %+v; want none and comment c9", j.writes, a)
	}
}

func TestWriterRetry(t *testing.T) {
	// Unrelated Target Versions are kept.
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
	want := []string{"edit PROJQUAY-1 quay-v3.17.z,quay-v3.18.2,omr-v2.0.z", "transition PROJQUAY-1 31", "transition PROJQUAY-1 31", "comment PROJQUAY-1"}
	if !slices.Equal(j.writes, want) {
		t.Errorf("writes %v; want %v", j.writes, want)
	}
	a = store.actions[writeKey]
	if a.Status != StatusApplied || a.Error != "" || a.TransitionStep != "transitioned" || a.StatusBefore != "Verified" || a.StatusAfter != releasePending || a.Attempts != 2 {
		t.Errorf("retry: action %+v; want applied on attempt 2", a)
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
			writes: []string{"edit PROJQUAY-1 quay-v3.18.2", "comment PROJQUAY-1"},
			check:  func(a Action) bool { return a.Status == StatusApplied && a.TransitionStep == "none from In Progress" },
		},
		"release pending not offered": {
			jira:   func(j *fakeJira) { j.transitions = j.transitions[:1] },
			writes: []string{"edit PROJQUAY-1 quay-v3.18.2", "comment PROJQUAY-1"},
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
