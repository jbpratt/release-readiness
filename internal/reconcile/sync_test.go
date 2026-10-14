package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	fbcs      []StageFBC
	members   map[string][]string
	sources   map[string][]Source
	unchecked map[string]bool
	baselines map[slot]Baseline
	actions   map[ActionKey]Action
	runs      map[[2]string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		members:   map[string][]string{},
		sources:   map[string][]Source{},
		unchecked: map[string]bool{},
		baselines: map[slot]Baseline{},
		actions:   map[ActionKey]Action{},
		runs:      map[[2]string]string{},
	}
}

func (f *fakeStore) UpsertSource(context.Context, Source) error { return nil }
func (f *fakeStore) SourceByRecord(context.Context, string) (Source, bool, error) {
	return Source{}, false, nil
}

func (f *fakeStore) SourcesByDigest(_ context.Context, digest string) ([]Source, error) {
	return f.sources[digest], nil
}

func (f *fakeStore) SourceState(_ context.Context, digest string) (bool, bool, error) {
	return len(f.sources[digest]) > 0 || f.unchecked[digest], f.unchecked[digest], nil
}

func (f *fakeStore) GetBaseline(_ context.Context, stream, operator, ocp string) (Baseline, bool, error) {
	b, ok := f.baselines[slot{stream, operator, ocp}]
	return b, ok, nil
}

func (f *fakeStore) InsertBaseline(_ context.Context, b Baseline) error {
	k := slot{b.Stream, b.Operator, b.OCP}
	if _, ok := f.baselines[k]; !ok {
		f.baselines[k] = b
	}
	return nil
}

func (f *fakeStore) UpsertAction(_ context.Context, a Action) error {
	if old, ok := f.actions[a.Key]; !ok || old.Status != StatusProposed {
		f.actions[a.Key] = a
	}
	return nil
}

func (f *fakeStore) ListActions(context.Context, string) ([]Action, error) { return nil, nil }

func (f *fakeStore) FirstIncludedAction(_ context.Context, stream, issueKey string) (Action, bool, error) {
	var first Action
	var found bool
	for _, a := range f.actions {
		if a.Key.IssueKey != issueKey || !strings.HasPrefix(a.Assembly, stream+".") || a.Status != StatusProposed {
			continue
		}
		if !found || a.UpdatedAt.Before(first.UpdatedAt) || a.UpdatedAt.Equal(first.UpdatedAt) && a.Key.Release < first.Key.Release {
			first, found = a, true
		}
	}
	return first, found, nil
}

func (f *fakeStore) ListStageFBCs(context.Context) ([]StageFBC, error) { return f.fbcs, nil }

func (f *fakeStore) FBCMemberDigests(_ context.Context, digest string) ([]string, error) {
	return f.members[digest], nil
}

func (f *fakeStore) RunDone(_ context.Context, release, digest string) (bool, error) {
	errText, ok := f.runs[[2]string{release, digest}]
	return ok && errText == "", nil
}

func (f *fakeStore) MarkRun(_ context.Context, release, digest, errText string) error {
	f.runs[[2]string{release, digest}] = errText
	return nil
}

type fakeCommits struct {
	ranges map[string][]Commit
	errs   map[string]error
	calls  []string
}

func (f *fakeCommits) Range(_ context.Context, repo, branch, base, head string) ([]Commit, error) {
	k := strings.Join([]string{repo, branch, base, head}, " ")
	f.calls = append(f.calls, k)
	if err := f.errs[k]; err != nil {
		return nil, err
	}
	return f.ranges[k], nil
}

func sha(n int) string { return fmt.Sprintf("%040x", n) }

const (
	quayRepo = "https://github.com/quay/quay"
	opRepo   = "https://github.com/quay/quay-operator"
)

type fixture struct {
	store   *fakeStore
	commits *fakeCommits
	sync    *Sync
	clock   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	fx := &fixture{store: newFakeStore(), commits: &fakeCommits{ranges: map[string][]Commit{}, errs: map[string]error{}}, clock: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	fx.sync = NewSync(cfg, fx.store, fx.commits, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fx.sync.now = func() time.Time { fx.clock = fx.clock.Add(time.Second); return fx.clock }
	return fx
}

// stage adds a STAGE FBC of quay-operator for OCP 4.16 whose members are
// itself, built from opCommit, and a quay image built from quayCommit.
func (fx *fixture) stage(release, assembly, digest string, quayCommit, opCommit string) StageFBC {
	f := StageFBC{
		Release:        release,
		Assembly:       assembly,
		CompletionTime: fmt.Sprintf("2026-10-14T%02d:00:00Z", len(fx.store.fbcs)),
		FBCDigest:      digest,
		FBCNVR:         "quay-operator-fbc-" + assembly + "-" + release + ".el9.ocp4.16",
	}
	fx.store.fbcs = append(fx.store.fbcs, f)
	quay := "sha256:quay-" + quayCommit
	fx.store.sources[digest] = []Source{{Name: "quay-operator-fbc", Digest: digest, SourceRepo: opRepo, Commitish: opCommit}}
	fx.store.sources[quay] = []Source{{Name: "quay-quay", Digest: quay, SourceRepo: quayRepo, Commitish: quayCommit}}
	fx.store.members[digest] = []string{quay, digest}
	return f
}

func (fx *fixture) run(t *testing.T) {
	t.Helper()
	if err := fx.sync.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRunOnce(t *testing.T) {
	fx := newFixture(t)
	op := sha(100)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), op)
	f2 := fx.stage("r2", "3.18.1", "sha256:f2", sha(2), op)
	fx.run(t)

	// The latest FBC is the baseline, reported for nothing.
	want := Baseline{Stream: "3.18", Operator: "quay-operator-fbc", OCP: "4.16", FBCDigest: "sha256:f2", FBCNVR: f2.FBCNVR}
	if got := fx.store.baselines[slot{"3.18", "quay-operator-fbc", "4.16"}]; got != want {
		t.Errorf("baseline = %+v; want %+v", got, want)
	}
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Errorf("first pass: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	if want := map[[2]string]string{{"r2", "sha256:f2"}: ""}; !reflect.DeepEqual(fx.store.runs, want) {
		t.Errorf("runs = %v; want only the baseline's", fx.store.runs)
	}

	f3 := fx.stage("r3", "3.18.2", "sha256:f3", sha(3), op)
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(2)+" "+sha(3)] = []Commit{
		{SHA: sha(3), Message: "PROJQUAY-1 fix\n\nSee PROJQUAY-1.", URL: "https://github.com/quay/quay/commit/" + sha(3)},
		{SHA: sha(30), Message: "chore: bump (PROJQUAY-2)", URL: "https://github.com/quay/quay/commit/" + sha(30)},
	}
	fx.run(t)
	for _, key := range []string{"PROJQUAY-1", "PROJQUAY-2"} {
		a, ok := fx.store.actions[ActionKey{FBCDigest: "sha256:f3", IssueKey: key, Release: "r3"}]
		if !ok || a.Status != StatusProposed || a.DesiredTargetVersion != "quay-v3.18.2" || a.BaselineDigest != "sha256:f2" || a.FBCNVR != f3.FBCNVR {
			t.Errorf("%s: %+v, %v; want proposed for quay-v3.18.2", key, a, ok)
		}
	}
	ev := fx.store.actions[ActionKey{FBCDigest: "sha256:f3", IssueKey: "PROJQUAY-1", Release: "r3"}].EvidenceJSON
	if want := `[{"component":"quay-quay","repo":"https://github.com/quay/quay","sha":"` + sha(3) + `","url":"https://github.com/quay/quay/commit/` + sha(3) + `"}]`; ev != want {
		t.Errorf("evidence = %s; want %s", ev, want)
	}
	if len(fx.store.actions) != 2 {
		t.Errorf("actions = %+v; want two", fx.store.actions)
	}

	// A respin whose range lists PROJQUAY-1 again inherits it.
	fx.stage("r4", "3.18.2", "sha256:f4", sha(4), op)
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(3)+" "+sha(4)] = []Commit{{SHA: sha(4), Message: "PROJQUAY-1 follow-up"}}
	fx.run(t)
	a := fx.store.actions[ActionKey{FBCDigest: "sha256:f4", IssueKey: "PROJQUAY-1", Release: "r4"}]
	if want := "already included in " + f3.FBCNVR + " (r3)"; a.Status != StatusSkipped || a.SkipReason != want || a.DesiredTargetVersion != "" {
		t.Errorf("inherited PROJQUAY-1 = %+v; want skipped %q", a, want)
	}
	if len(fx.store.actions) != 3 {
		t.Errorf("actions = %+v; want three", fx.store.actions)
	}

	// Done runs are not fetched or rewritten again.
	before := maps.Clone(fx.store.actions)
	fx.commits.calls = nil
	fx.run(t)
	if len(fx.commits.calls) != 0 || !reflect.DeepEqual(fx.store.actions, before) {
		t.Errorf("rerun: fetches %v, actions changed %v", fx.commits.calls, !reflect.DeepEqual(fx.store.actions, before))
	}
}

func TestRunOnceUncertain(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.run(t)

	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(101))
	fx.store.members["sha256:f2"] = append(fx.store.members["sha256:f2"], "sha256:unknown")
	fx.commits.errs[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = errors.New("head is not on redhat-3.18")
	fx.commits.ranges[opRepo+" redhat-3.18 "+sha(100)+" "+sha(101)] = []Commit{
		{SHA: sha(101), Message: "PROJQUAY-7: fix"},
		{SHA: sha(102), Message: "PROJQUAY-8: fix"},
		{SHA: sha(103), Message: "Revert \"PROJQUAY-8: fix\""},
	}
	fx.run(t)

	if a := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", IssueKey: "PROJQUAY-7", Release: "r2"}]; a.Status != StatusProposed {
		t.Errorf("verified key = %+v; want proposed", a)
	}
	u := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", Release: "r2"}]
	if want := "PROJQUAY-8: reverted by " + sha(103) + "; component quay-quay: head is not on redhat-3.18; component sha256:unknown: no ART source"; u.Status != StatusUncertain || u.SkipReason != want {
		t.Errorf("uncertain = %+v; want reason %q", u, want)
	}
	if len(fx.store.actions) != 2 {
		t.Errorf("actions = %+v; want two", fx.store.actions)
	}
}

func TestRunOnceUnslotted(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.15.1", "sha256:old", sha(1), sha(100))
	f := fx.stage("r2", "3.18.1", "sha256:noocp", sha(2), sha(100))
	f.FBCNVR = "quay-operator-fbc-3.18.1-1"
	fx.store.fbcs[1] = f
	fx.run(t)

	if len(fx.store.baselines) != 0 || len(fx.store.actions) != 1 {
		t.Fatalf("baselines %+v, actions %+v; want only the NVR's uncertainty", fx.store.baselines, fx.store.actions)
	}
	u := fx.store.actions[ActionKey{FBCDigest: "sha256:noocp", Release: "r2"}]
	if u.Status != StatusUncertain || !strings.Contains(u.SkipReason, "no ocp4.N") {
		t.Errorf("uncertain = %+v; want no ocp4.N", u)
	}
	if _, ok := fx.store.runs[[2]string{"r1", "sha256:old"}]; ok {
		t.Error("3.15 FBC marked run")
	}
}

func TestRunOnceUnchangedMember(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.store.members["sha256:f1"] = append(fx.store.members["sha256:f1"], "sha256:ext1")
	fx.run(t)

	// An external image carried over unchanged is not uncertain.
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(100))
	fx.store.members["sha256:f2"] = append(fx.store.members["sha256:f2"], "sha256:ext1")
	fx.run(t)
	if len(fx.store.actions) != 0 {
		t.Errorf("actions = %+v; want none", fx.store.actions)
	}

	fx.stage("r3", "3.18.3", "sha256:f3", sha(3), sha(100))
	fx.store.members["sha256:f3"] = append(fx.store.members["sha256:f3"], "sha256:ext2")
	fx.run(t)
	u := fx.store.actions[ActionKey{FBCDigest: "sha256:f3", Release: "r3"}]
	if want := "component sha256:ext2: no ART source"; u.Status != StatusUncertain || u.SkipReason != want {
		t.Errorf("uncertain = %+v; want reason %q", u, want)
	}
}

func TestRunOnceAwaitsProvenance(t *testing.T) {
	fx := newFixture(t)
	op := sha(100)
	// The FBC's own record is not checked yet: its stream waits.
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), op)
	fbcSrc := fx.store.sources["sha256:f1"]
	delete(fx.store.sources, "sha256:f1")
	fx.store.unchecked["sha256:f1"] = true
	fx.run(t)
	if len(fx.store.baselines) != 0 || len(fx.store.actions) != 0 {
		t.Fatalf("unchecked FBC: baselines %+v, actions %+v; want none", fx.store.baselines, fx.store.actions)
	}
	fx.store.sources["sha256:f1"], fx.store.unchecked["sha256:f1"] = fbcSrc, false
	fx.run(t)
	if len(fx.store.baselines) != 1 {
		t.Fatalf("baselines %+v; want f1's", fx.store.baselines)
	}

	// A changed member's record is not checked yet: it and the FBC after it wait.
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), op)
	quay2 := "sha256:quay-" + sha(2)
	quaySrc := fx.store.sources[quay2]
	delete(fx.store.sources, quay2)
	fx.store.unchecked[quay2] = true
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-7 fix"}}
	fx.run(t)
	fx.stage("r3", "3.18.3", "sha256:f3", sha(3), op)
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(2)+" "+sha(3)] = []Commit{{SHA: sha(3), Message: "PROJQUAY-8 fix"}}
	fx.run(t)
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Fatalf("unchecked member: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	fx.store.sources[quay2], fx.store.unchecked[quay2] = quaySrc, false
	fx.run(t)
	for k, want := range map[ActionKey]string{
		{FBCDigest: "sha256:f2", IssueKey: "PROJQUAY-7", Release: "r2"}: "quay-v3.18.2",
		{FBCDigest: "sha256:f3", IssueKey: "PROJQUAY-8", Release: "r3"}: "quay-v3.18.3",
	} {
		if a := fx.store.actions[k]; a.Status != StatusProposed || a.DesiredTargetVersion != want {
			t.Errorf("%v: %+v; want proposed for %s", k, a, want)
		}
	}
	if len(fx.store.actions) != 2 {
		t.Errorf("actions = %+v; want two", fx.store.actions)
	}

	// A member with no record waits provenanceWait after its Release completed.
	f4 := fx.stage("r4", "3.18.4", "sha256:f4", sha(3), op)
	f4.CompletionTime = fx.clock.Format(time.RFC3339)
	fx.store.fbcs[len(fx.store.fbcs)-1] = f4
	fx.store.members["sha256:f4"] = append(fx.store.members["sha256:f4"], "sha256:ext")
	fx.run(t)
	u := ActionKey{FBCDigest: "sha256:f4", Release: "r4"}
	if a, ok := fx.store.actions[u]; ok {
		t.Fatalf("within the wait: %+v; want none", a)
	}
	fx.clock = fx.clock.Add(provenanceWait)
	fx.run(t)
	if a := fx.store.actions[u]; a.Status != StatusUncertain || a.SkipReason != "component sha256:ext: no ART source" {
		t.Errorf("after the wait: %+v; want uncertain", a)
	}
}

func TestRunOnceHeldStream(t *testing.T) {
	fx := newFixture(t)
	var logs bytes.Buffer
	fx.sync.logger = slog.New(slog.NewTextHandler(&logs, nil))
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.run(t)
	// f2 has no slot until its record is checked; f3, in the same stream, waits behind it.
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(100))
	fbcSrc := fx.store.sources["sha256:f2"]
	delete(fx.store.sources, "sha256:f2")
	fx.store.unchecked["sha256:f2"] = true
	fx.stage("r3", "3.18.3", "sha256:f3", sha(3), sha(100))
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-7 fix"}}
	fx.run(t)
	fx.run(t)
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Fatalf("held: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	if n := strings.Count(logs.String(), "msg=waiting"); n != 1 {
		t.Errorf("logged the wait %d times; want once:\n%s", n, logs.String())
	}
	fx.store.sources["sha256:f2"], fx.store.unchecked["sha256:f2"] = fbcSrc, false
	fx.run(t)
	if a := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", IssueKey: "PROJQUAY-7", Release: "r2"}]; a.Status != StatusProposed || a.DesiredTargetVersion != "quay-v3.18.2" {
		t.Errorf("PROJQUAY-7 = %+v; want proposed on f2", a)
	}
}

func TestRunOnceAwaitsPrevMember(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.run(t)
	// f1's quay image, replaced in f2, has an unchecked record.
	quay1 := "sha256:quay-" + sha(1)
	quaySrc := fx.store.sources[quay1]
	delete(fx.store.sources, quay1)
	fx.store.unchecked[quay1] = true
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(100))
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-7 fix"}}
	fx.run(t)
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Fatalf("unchecked prev member: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	fx.store.sources[quay1], fx.store.unchecked[quay1] = quaySrc, false
	fx.run(t)
	if a := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", IssueKey: "PROJQUAY-7", Release: "r2"}]; a.Status != StatusProposed {
		t.Errorf("PROJQUAY-7 = %+v; want proposed", a)
	}
}

func TestRunOnceLateFBC(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), sha(100))
	fx.run(t)
	fx.stage("r2", "3.18.2", "sha256:f2", sha(2), sha(100))
	f3 := fx.stage("r3", "3.18.3", "sha256:f3", sha(3), sha(100))
	// f2 renders late, after f3 was reconciled against f1.
	fx.store.fbcs = slices.Delete(fx.store.fbcs, 1, 2)
	fx.run(t)
	fx.store.fbcs = slices.Insert(fx.store.fbcs, 1, StageFBC{Release: "r2", Assembly: "3.18.2", CompletionTime: "2026-10-14T01:00:00Z", FBCDigest: "sha256:f2", FBCNVR: "quay-operator-fbc-3.18.2-r2.el9.ocp4.16"})
	fx.run(t)
	a := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", Release: "r2"}]
	if want := "staged before " + f3.FBCNVR + ", which was reconciled first"; a.Status != StatusUncertain || a.SkipReason != want {
		t.Errorf("late FBC: %+v; want uncertain %q", a, want)
	}
}
