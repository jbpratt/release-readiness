package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
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
	runs      map[[2]string]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		members:   map[string][]string{},
		sources:   map[string][]Source{},
		unchecked: map[string]bool{},
		baselines: map[slot]Baseline{},
		actions:   map[ActionKey]Action{},
		runs:      map[[2]string]bool{},
	}
}

func (f *fakeStore) SourcesByDigest(_ context.Context, digest string) ([]Source, error) {
	return f.sources[digest], nil
}

func (f *fakeStore) SourceState(_ context.Context, digest string) (bool, bool, error) {
	return len(f.sources[digest]) > 0 || f.unchecked[digest], f.unchecked[digest], nil
}

func (f *fakeStore) GetBaseline(_ context.Context, stream, operator, ocp string) (string, bool, error) {
	b, ok := f.baselines[slot{stream, operator, ocp}]
	return b.FBCDigest, ok, nil
}

func (f *fakeStore) InsertBaseline(_ context.Context, b Baseline) error {
	k := slot{b.Stream, b.Operator, b.OCP}
	if _, ok := f.baselines[k]; !ok {
		f.baselines[k] = b
	}
	return nil
}

func (f *fakeStore) UpsertAction(_ context.Context, a Action) error {
	old, ok := f.actions[a.Key]
	if !ok || a.Attempts == 0 && old.Attempts == 0 && old.Status != StatusProposed ||
		a.Attempts > 0 && a.Attempts >= old.Attempts && (old.Status == StatusProposed || old.Status == StatusError) {
		f.actions[a.Key] = a
	}
	return nil
}

func (f *fakeStore) PendingActions(_ context.Context, maxAttempts int) ([]Action, error) {
	var pending []Action
	for _, a := range f.actions {
		if a.Status == StatusProposed || a.Status == StatusError && a.Attempts < maxAttempts {
			pending = append(pending, a)
		}
	}
	return pending, nil
}

func (f *fakeStore) FirstIncludedAction(_ context.Context, stream, issueKey string) (Action, bool, error) {
	var first Action
	var found bool
	for _, a := range f.actions {
		if a.Key.IssueKey != issueKey || !strings.HasPrefix(a.Assembly, stream+".") || !slices.Contains([]string{StatusProposed, StatusApplied, StatusError}, a.Status) {
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
	return f.runs[[2]string{release, digest}], nil
}

func (f *fakeStore) MarkRun(_ context.Context, release, digest string) error {
	f.runs[[2]string{release, digest}] = true
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
	logs    bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{store: newFakeStore(), commits: &fakeCommits{ranges: map[string][]Commit{}, errs: map[string]error{}}, clock: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	fx.sync = NewSync(fx.store, fx.commits, slog.New(slog.NewTextHandler(&fx.logs, nil)))
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
		RenderState:    "rendered",
	}
	fx.store.fbcs = append(fx.store.fbcs, f)
	quay := "sha256:quay-" + quayCommit
	fx.store.sources[digest] = []Source{{Name: "quay-operator-fbc", SourceRepo: opRepo, Commitish: opCommit}}
	fx.store.sources[quay] = []Source{{Name: "quay-quay", SourceRepo: quayRepo, Commitish: quayCommit}}
	fx.store.members[digest] = []string{quay, digest}
	return f
}

func (fx *fixture) run(t *testing.T) {
	t.Helper()
	if err := fx.sync.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// uncertain returns the reasons logged for release's FBC, "" for none.
func (fx *fixture) uncertain(release string) string {
	for line := range strings.Lines(fx.logs.String()) {
		if _, reasons, ok := strings.Cut(line, "msg=uncertain release="+release+" "); ok {
			_, reasons, _ = strings.Cut(reasons, "reasons=")
			return strings.Trim(strings.TrimSpace(reasons), `"`)
		}
	}
	return ""
}

func TestRunOnce(t *testing.T) {
	fx := newFixture(t)
	op := sha(100)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), op)
	fx.stage("r2", "3.18.1", "sha256:f2", sha(2), op)
	fx.run(t)

	// The latest FBC is the baseline, reported for nothing.
	want := Baseline{Stream: "3.18", Operator: "quay-operator-fbc", OCP: "4.16", FBCDigest: "sha256:f2"}
	if got := fx.store.baselines[slot{"3.18", "quay-operator-fbc", "4.16"}]; got != want {
		t.Errorf("baseline = %+v; want %+v", got, want)
	}
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Errorf("first pass: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	if want := map[[2]string]bool{{"r2", "sha256:f2"}: true}; !reflect.DeepEqual(fx.store.runs, want) {
		t.Errorf("runs = %v; want only the baseline's", fx.store.runs)
	}

	f3 := fx.stage("r3", "3.18.2", "sha256:f3", sha(3), op)
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(2)+" "+sha(3)] = []Commit{
		{SHA: sha(3), Message: "PROJQUAY-1 fix\n\nSee PROJQUAY-1."},
		{SHA: sha(30), Message: "chore: bump (PROJQUAY-2)"},
	}
	fx.run(t)
	for _, key := range []string{"PROJQUAY-1", "PROJQUAY-2"} {
		a, ok := fx.store.actions[ActionKey{FBCDigest: "sha256:f3", IssueKey: key, Release: "r3"}]
		if !ok || a.Status != StatusProposed || a.DesiredTargetVersion != "quay-v3.18.2" || a.FBCNVR != f3.FBCNVR {
			t.Errorf("%s: %+v, %v; want proposed for quay-v3.18.2", key, a, ok)
		}
	}
	ev := fx.store.actions[ActionKey{FBCDigest: "sha256:f3", IssueKey: "PROJQUAY-1", Release: "r3"}].EvidenceJSON
	if want := `[{"component":"quay-quay","repo":"https://github.com/quay/quay","sha":"` + sha(3) + `"}]`; ev != want {
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

	// Each proposed action is logged once; the skipped one is not.
	logs := fx.logs.String()
	if n := strings.Count(logs, "msg=proposed "); n != 2 {
		t.Errorf("logged %d proposed actions; want 2:\n%s", n, logs)
	}
	if want := "msg=proposed issue=PROJQUAY-1 fbc=" + f3.FBCNVR + " release=r3 target_version_add=quay-v3.18.2 target_version_remove=quay-v3.18.z"; !strings.Contains(logs, want) {
		t.Errorf("logs lack %q:\n%s", want, logs)
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
	if got, want := fx.uncertain("r2"), "PROJQUAY-8: reverted by "+sha(103)+"; component quay-quay: head is not on redhat-3.18; component sha256:unknown: no ART source"; got != want {
		t.Errorf("uncertain = %q; want %q", got, want)
	}
	if len(fx.store.actions) != 1 {
		t.Errorf("actions = %+v; want one", fx.store.actions)
	}
}

func TestRunOnceUnslotted(t *testing.T) {
	fx := newFixture(t)
	fx.stage("r1", "3.15.1", "sha256:old", sha(1), sha(100))
	f := fx.stage("r2", "3.18.1", "sha256:noocp", sha(2), sha(100))
	f.FBCNVR = "quay-operator-fbc-3.18.1-1"
	fx.store.fbcs[1] = f
	fx.run(t)

	if len(fx.store.baselines) != 0 || len(fx.store.actions) != 0 {
		t.Fatalf("baselines %+v, actions %+v; want none", fx.store.baselines, fx.store.actions)
	}
	if u := fx.uncertain("r2"); !strings.Contains(u, "no ocpX.Y") {
		t.Errorf("uncertain = %q; want no ocpX.Y", u)
	}
	if !fx.store.runs[[2]string{"r2", "sha256:noocp"}] {
		t.Error("unslotted FBC not marked run")
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
	if u := fx.uncertain("r2"); u != "" {
		t.Errorf("r2 uncertain: %q; want none", u)
	}
	if got, want := fx.uncertain("r3"), "component sha256:ext2: no ART source"; got != want {
		t.Errorf("uncertain = %q; want %q", got, want)
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
	if u := fx.uncertain("r4"); u != "" || fx.store.runs[[2]string{"r4", "sha256:f4"}] {
		t.Fatalf("within the wait: uncertain %q, done %v; want neither", u, fx.store.runs[[2]string{"r4", "sha256:f4"}])
	}
	fx.clock = fx.clock.Add(provenanceWait)
	fx.run(t)
	if got, want := fx.uncertain("r4"), "component sha256:ext: no ART source"; got != want {
		t.Errorf("after the wait: uncertain %q; want %q", got, want)
	}
}

func TestRunOnceHeldStream(t *testing.T) {
	fx := newFixture(t)
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
	if n := strings.Count(fx.logs.String(), "msg=waiting"); n != 1 {
		t.Errorf("logged the wait %d times; want once:\n%s", n, fx.logs.String())
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
	fx.store.fbcs = slices.Insert(fx.store.fbcs, 1, StageFBC{Release: "r2", Assembly: "3.18.2", CompletionTime: "2026-10-14T01:00:00Z", FBCDigest: "sha256:f2", FBCNVR: "quay-operator-fbc-3.18.2-r2.el9.ocp4.16", RenderState: "rendered"})
	fx.run(t)
	if got, want := fx.uncertain("r2"), "staged before "+f3.FBCNVR+", which was reconciled first"; got != want {
		t.Errorf("late FBC: uncertain %q; want %q", got, want)
	}
}

func TestRunOnceGap(t *testing.T) {
	for name, unslot := range map[string]func(*StageFBC){
		"no slot":       func(f *StageFBC) { f.FBCNVR = "quay-operator-fbc-3.18.2-1" },
		"render failed": func(f *StageFBC) { f.RenderState = "failed" },
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			op := sha(100)
			fx.stage("r1", "3.18.1", "sha256:f1", sha(1), op)
			fx.run(t)
			// f3's slot predecessor is f1, so its diff would take f2's PROJQUAY-2.
			f2 := fx.stage("r2", "3.18.2", "sha256:f2", sha(2), op)
			unslot(&f2)
			fx.store.fbcs[1] = f2
			fx.stage("r3", "3.18.3", "sha256:f3", sha(3), op)
			fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(3)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-2 fix"}}
			fx.run(t)
			if u := fx.uncertain("r2"); u == "" {
				t.Error("f2 not uncertain")
			}
			if got, want := fx.uncertain("r3"), "follows "+f2.FBCNVR+" (r2), which has no slot"; got != want {
				t.Errorf("f3 uncertain %q; want %q", got, want)
			}
			if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
				t.Errorf("actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
			}

			// The FBC after f3 is diffed against it.
			fx.stage("r4", "3.18.4", "sha256:f4", sha(4), op)
			fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(3)+" "+sha(4)] = []Commit{{SHA: sha(4), Message: "PROJQUAY-4 fix"}}
			fx.run(t)
			if a := fx.store.actions[ActionKey{FBCDigest: "sha256:f4", IssueKey: "PROJQUAY-4", Release: "r4"}]; a.Status != StatusProposed || a.DesiredTargetVersion != "quay-v3.18.4" {
				t.Errorf("PROJQUAY-4 = %+v; want proposed for quay-v3.18.4", a)
			}
		})
	}
}

// A Release stages an FBC per OCP version, listed together: a gap reaches
// the next FBC of every slot of its stream, not its siblings.
func TestRunOnceGapSlots(t *testing.T) {
	// r2's failed 4.16 FBC is listed before or after its 4.17 sibling.
	for _, failedFirst := range []bool{true, false} {
		fx := newFixture(t)
		op := sha(100)
		add := func(release, assembly, digest string, n int, ocp string) StageFBC {
			fx.stage(release, assembly, digest, sha(n), op)
			f := &fx.store.fbcs[len(fx.store.fbcs)-1]
			f.FBCNVR = "quay-operator-fbc-" + assembly + "-" + release + ".el9.ocp" + ocp
			return *f
		}
		add("r1", "3.18.1", "sha256:a1", 1, "4.16")
		add("r1", "3.18.1", "sha256:b1", 1, "4.17")
		fx.run(t)
		if !failedFirst {
			add("r2", "3.18.2", "sha256:b2", 2, "4.17")
		}
		a2 := add("r2", "3.18.2", "sha256:a2", 2, "4.16")
		fx.store.fbcs[len(fx.store.fbcs)-1].RenderState = "failed"
		if failedFirst {
			add("r2", "3.18.2", "sha256:b2", 2, "4.17")
		}
		add("r3", "3.18.3", "sha256:a3", 3, "4.16")
		add("r3", "3.18.3", "sha256:b3", 3, "4.17")
		fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-2 fix"}}
		fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(3)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-2 fix"}}
		fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(2)+" "+sha(3)] = []Commit{{SHA: sha(3), Message: "PROJQUAY-3 fix"}}
		fx.run(t)
		if got, want := fx.uncertain("r3"), "follows "+a2.FBCNVR+" (r2), which has no slot"; got != want {
			t.Errorf("failedFirst=%v: a3 uncertain %q; want %q", failedFirst, got, want)
		}
		for k, want := range map[ActionKey]string{
			{FBCDigest: "sha256:b2", IssueKey: "PROJQUAY-2", Release: "r2"}: "quay-v3.18.2",
			{FBCDigest: "sha256:b3", IssueKey: "PROJQUAY-3", Release: "r3"}: "quay-v3.18.3",
		} {
			if a := fx.store.actions[k]; a.Status != StatusProposed || a.DesiredTargetVersion != want {
				t.Errorf("failedFirst=%v: %v: %+v; want proposed for %s", failedFirst, k, a, want)
			}
		}
		if len(fx.store.actions) != 2 {
			t.Errorf("failedFirst=%v: actions = %+v; want b2 and b3 proposed", failedFirst, fx.store.actions)
		}
	}
}

func TestRunOnceUnrendered(t *testing.T) {
	fx := newFixture(t)
	op := sha(100)
	fx.stage("r1", "3.18.1", "sha256:f1", sha(1), op)
	fx.run(t)
	// f2 is not rendered yet: f3, in the same stream, waits behind it.
	f2 := fx.stage("r2", "3.18.2", "sha256:f2", sha(2), op)
	f2.RenderState = ""
	fx.store.fbcs[1] = f2
	fx.stage("r3", "3.18.3", "sha256:f3", sha(3), op)
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(1)+" "+sha(2)] = []Commit{{SHA: sha(2), Message: "PROJQUAY-7 fix"}}
	fx.commits.ranges[quayRepo+" redhat-3.18 "+sha(2)+" "+sha(3)] = []Commit{{SHA: sha(3), Message: "PROJQUAY-8 fix"}}
	fx.run(t)
	if len(fx.store.actions) != 0 || len(fx.commits.calls) != 0 {
		t.Fatalf("unrendered: actions %+v, fetches %v; want none", fx.store.actions, fx.commits.calls)
	}
	fx.store.fbcs[1].RenderState = "rendered"
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
}

// A transient GitHub error leaves the FBC to the next pass; a permanent one
// is its uncertainty.
func TestRunOnceGitHubError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		retry  bool
	}{
		{"unavailable", http.StatusServiceUnavailable, true},
		{"rate limited", http.StatusTooManyRequests, true},
		{"not found", http.StatusNotFound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t)
			g := newFake(t)
			fx.sync.commits = g.start(t, "")
			op := sha(100)
			fx.stage("r1", "3.18.1", "sha256:f1", base, op)
			fx.run(t)
			fx.stage("r2", "3.18.2", "sha256:f2", head, op)
			g.fail = func(w http.ResponseWriter) { w.WriteHeader(tc.status) }
			fx.run(t)
			done := fx.store.runs[[2]string{"r2", "sha256:f2"}]
			if len(fx.store.actions) != 0 || done == tc.retry {
				t.Fatalf("GitHub %d: actions %+v, done %v; want none, done %v", tc.status, fx.store.actions, done, !tc.retry)
			}
			if u := fx.uncertain("r2"); (u == "") != tc.retry {
				t.Errorf("GitHub %d: uncertain %q", tc.status, u)
			}

			g.fail = nil
			fx.run(t)
			if !tc.retry {
				if len(fx.store.actions) != 0 {
					t.Errorf("done FBC proposed %+v", fx.store.actions)
				}
				return
			}
			for _, key := range []string{"PROJQUAY-1", "PROJQUAY-2"} {
				if a := fx.store.actions[ActionKey{FBCDigest: "sha256:f2", IssueKey: key, Release: "r2"}]; a.Status != StatusProposed || a.DesiredTargetVersion != "quay-v3.18.2" {
					t.Errorf("retry: %s = %+v; want proposed for quay-v3.18.2", key, a)
				}
			}
		})
	}
}
