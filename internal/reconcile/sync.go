package reconcile

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

// provenanceWait bounds how long after its Release completed an FBC waits
// for ART records of its digests that are not stored yet: the history sync
// lists them, and the FBC render stores its bundle's after the render.
const provenanceWait = 6 * time.Hour

var (
	issueKey = regexp.MustCompile(`\bPROJQUAY-[0-9]+\b`)
	ocpToken = regexp.MustCompile(`\bocp([0-9]+\.[0-9]+)\b`)
)

// Sync proposes, for each successful STAGE FBC after its slot's baseline,
// the Jira issues fixed by the commits between it and the previous one.
type Sync struct {
	store   Store
	commits CommitFetcher
	logger  *slog.Logger
	now     func() time.Time
	// waiting holds the reason each waiting FBC last logged.
	waiting map[[2]string]string
	// Tx runs fn in a transaction; nil runs it on the store directly.
	Tx func(ctx context.Context, fn func(Store) error) error
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
	// Writer, when set, applies the pending actions after each pass.
	Writer *Writer
}

func NewSync(store Store, commits CommitFetcher, log *slog.Logger) *Sync {
	return &Sync{store: store, commits: commits, logger: log, now: time.Now, waiting: map[[2]string]string{}}
}

// Run reconciles immediately and then every interval until ctx is cancelled.
func (s *Sync) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		err := s.RunOnce(ctx)
		if err != nil {
			s.logger.Error("reconcile", "error", err)
		}
		s.Status.Report(err)
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
		}
	}
}

type slot struct{ stream, operator, ocp string }

// RunOnce reconciles every StageFBC not yet done, then writes the pending
// actions to Jira when a Writer is set. Only store errors fail it;
// anything that keeps an FBC's issues from being known for good is logged
// as its uncertainty.
func (s *Sync) RunOnce(ctx context.Context) error {
	fbcs, err := s.store.ListStageFBCs(ctx)
	if err != nil {
		return err
	}
	slots := map[slot][]StageFBC{}
	var order []slot
	held := map[string]bool{}
	// noSlot lists each stream's FBCs with no slot. Any slot's next FBC may
	// follow one, so a diff against its slot predecessor would take its fixes.
	noSlot := map[string][]StageFBC{}
	// seen counts the stream's noSlot FBCs listed before each slot's last FBC.
	seen := map[slot]int{}
	// gaps holds why each slotted FBC listed after one of those is uncertain.
	gaps := map[[2]string]string{}
	for _, f := range fbcs {
		stream := f.Assembly
		if i := strings.LastIndex(stream, "."); i >= 0 {
			stream = stream[:i]
		}
		if !slices.Contains(streams, stream) {
			s.logger.Debug("stream not configured", "release", f.Release, "assembly", f.Assembly)
			continue
		}
		if f.RenderState == "" {
			// f's slot and members are not known yet, so its whole stream waits with it.
			held[stream] = true
			s.wait(f, "waiting for the FBC render")
			continue
		}
		var sl slot
		reason := fmt.Sprintf("FBC %s: render %s", f.FBCDigest, f.RenderState)
		if f.RenderState == "rendered" {
			if sl, reason, err = s.slotOf(ctx, stream, f); err != nil {
				return err
			}
		}
		if reason == "" {
			var prev string
			if p := slots[sl]; len(p) > 0 {
				prev = p[len(p)-1].Release
			} else {
				order = append(order, sl)
			}
			slots[sl] = append(slots[sl], f)
			for _, u := range noSlot[stream][seen[sl]:] {
				// u's siblings in its Release, f's or its slot predecessor's, are FBCs of other slots.
				if u.Release != f.Release && u.Release != prev {
					gaps[[2]string{f.Release, f.FBCDigest}] = fmt.Sprintf("follows %s (%s), which has no slot", cmp.Or(u.FBCNVR, u.FBCDigest), u.Release)
				}
			}
			seen[sl] = len(noSlot[stream])
			continue
		}
		done, err := s.store.RunDone(ctx, f.Release, f.FBCDigest)
		if err != nil {
			return err
		}
		if !done && f.RenderState == "rendered" {
			wait, err := s.awaiting(ctx, f, f.FBCDigest)
			if err != nil {
				return err
			}
			if wait {
				// f's slot is not known yet, so its whole stream waits with it.
				held[stream] = true
				s.wait(f, "waiting for ART source: "+reason)
				continue
			}
		}
		noSlot[stream] = append(noSlot[stream], f)
		if done {
			continue
		}
		s.uncertain(f, reason)
		if err := s.record(ctx, f, nil); err != nil {
			return err
		}
	}
	for _, sl := range order {
		if held[sl.stream] {
			continue
		}
		if err := s.reconcileSlot(ctx, sl, slots[sl], gaps); err != nil {
			return err
		}
	}
	if s.Writer != nil {
		return s.Writer.ApplyPending(ctx)
	}
	return nil
}

// slotOf returns f's slot, or why it has none.
func (s *Sync) slotOf(ctx context.Context, stream string, f StageFBC) (slot, string, error) {
	srcs, err := s.store.SourcesByDigest(ctx, f.FBCDigest)
	if err != nil {
		return slot{}, "", err
	}
	var names []string
	for _, src := range srcs {
		names = append(names, src.Name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) != 1 {
		return slot{}, fmt.Sprintf("FBC %s: %d ART names %v, want one", f.FBCDigest, len(names), names), nil
	}
	m := ocpToken.FindStringSubmatch(f.FBCNVR)
	if m == nil {
		return slot{}, fmt.Sprintf("FBC %s: no ocpX.Y in NVR %q", f.FBCDigest, f.FBCNVR), nil
	}
	return slot{stream: stream, operator: names[0], ocp: m[1]}, "", nil
}

// reconcileSlot freezes the slot's baseline on first sight, then reconciles
// each FBC after it against the one before, oldest first. An FBC in gaps
// is uncertain for its reason instead.
func (s *Sync) reconcileSlot(ctx context.Context, sl slot, fbcs []StageFBC, gaps map[[2]string]string) error {
	base, ok, err := s.store.GetBaseline(ctx, sl.stream, sl.operator, sl.ocp)
	if err != nil {
		return err
	}
	if !ok {
		latest := fbcs[len(fbcs)-1]
		s.logger.Info("baseline", "stream", sl.stream, "operator", sl.operator, "ocp", sl.ocp, "fbc", latest.FBCNVR, "release", latest.Release)
		return s.tx(ctx, func(st Store) error {
			if err := st.InsertBaseline(ctx, Baseline{Stream: sl.stream, Operator: sl.operator, OCP: sl.ocp, FBCDigest: latest.FBCDigest}); err != nil {
				return err
			}
			return st.MarkRun(ctx, latest.Release, latest.FBCDigest)
		})
	}
	// A baseline no longer listed was older than every FBC listed.
	start := 0
	for i, f := range fbcs {
		if f.FBCDigest == base {
			start = i + 1
		}
	}
	done := make([]bool, len(fbcs))
	last := -1
	for i := start; i < len(fbcs); i++ {
		if done[i], err = s.store.RunDone(ctx, fbcs[i].Release, fbcs[i].FBCDigest); err != nil {
			return err
		}
		if done[i] {
			last = i
		}
	}
	for i := start; i < len(fbcs); i++ {
		f := fbcs[i]
		if done[i] {
			continue
		}
		if i < last {
			// f came after a later FBC was reconciled, which may hold its fixes.
			s.uncertain(f, fmt.Sprintf("staged before %s, which was reconciled first", fbcs[last].FBCNVR))
			if err := s.record(ctx, f, nil); err != nil {
				return err
			}
			continue
		}
		if gap := gaps[[2]string{f.Release, f.FBCDigest}]; gap != "" {
			s.uncertain(f, gap)
			if err := s.record(ctx, f, nil); err != nil {
				return err
			}
			continue
		}
		prev := base
		if i > 0 {
			prev = fbcs[i-1].FBCDigest
		}
		actions, wait, err := s.actions(ctx, sl.stream, f, prev)
		if err != nil {
			return err
		}
		if wait != "" {
			// The FBCs after f are diffed against it, so they wait with it.
			s.wait(f, wait)
			return nil
		}
		if err := s.record(ctx, f, actions); err != nil {
			return err
		}
	}
	return nil
}

type evidence struct {
	Component string `json:"component"`
	Repo      string `json:"repo"`
	SHA       string `json:"sha"`
}

// actions returns the actions for the issues in the commits between the
// members of prev and of f. A member already in prev changed nothing, so
// only new member digests are checked, and what keeps f's issues from being
// known is logged. While the ART source of a changed member may still
// arrive, or after a transient GitHub error, it returns why f waits instead.
func (s *Sync) actions(ctx context.Context, stream string, f StageFBC, prev string) ([]Action, string, error) {
	var uncertain []string
	prevSources, err := s.memberSources(ctx, prev)
	if err != nil {
		return nil, "", err
	}
	cur, err := s.memberSources(ctx, f.FBCDigest)
	if err != nil {
		return nil, "", err
	}
	for _, m := range [][2]map[string][]Source{{prevSources, cur}, {cur, prevSources}} {
		for digest, srcs := range m[0] {
			if _, ok := m[1][digest]; ok || len(srcs) > 0 {
				continue
			}
			wait, err := s.awaiting(ctx, f, digest)
			if err != nil {
				return nil, "", err
			}
			if wait {
				return nil, "waiting for ART source of " + digest, nil
			}
		}
	}
	before := map[string]Source{}
	ambiguous := map[string]bool{}
	for _, srcs := range prevSources {
		for _, src := range srcs {
			if p, ok := before[src.Name]; ok && p.Commitish != src.Commitish {
				ambiguous[src.Name] = true
			}
			before[src.Name] = src
		}
	}
	type span struct{ repo, base, head string }
	type spanOf struct {
		branch     string
		components []string
	}
	spans := map[span]*spanOf{}
	for digest, srcs := range cur {
		if _, ok := prevSources[digest]; ok {
			continue
		}
		if len(srcs) == 0 {
			uncertain = append(uncertain, fmt.Sprintf("component %s: no ART source", digest))
		}
		for _, src := range srcs {
			repo, branch, reason := allowlisted(src, stream)
			if reason == "" {
				p, ok := before[src.Name]
				switch {
				case !ok:
					reason = "new component"
				case ambiguous[src.Name]:
					reason = "more than one commit in the previous FBC"
				case p.SourceRepo != repo:
					reason = fmt.Sprintf("previous source repo %s is not %s", p.SourceRepo, repo)
				default:
					k := span{repo, p.Commitish, src.Commitish}
					if spans[k] == nil {
						spans[k] = &spanOf{branch: branch}
					}
					spans[k].components = append(spans[k].components, src.Name)
				}
			}
			if reason != "" {
				uncertain = append(uncertain, fmt.Sprintf("component %s: %s", src.Name, reason))
			}
		}
	}

	found := map[string][]evidence{}
	reverted := map[string]bool{}
	for k, sp := range spans {
		slices.Sort(sp.components)
		components := strings.Join(slices.Compact(sp.components), ", ")
		commits, err := s.commits.Range(ctx, k.repo, sp.branch, k.base, k.head)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			if errors.Is(err, errTransient) {
				return nil, "GitHub: " + err.Error(), nil
			}
			uncertain = append(uncertain, fmt.Sprintf("component %s: %v", components, err))
			continue
		}
		for _, c := range commits {
			for _, key := range issueKey.FindAllString(c.Message, -1) {
				if strings.HasPrefix(c.Message, "Revert ") {
					reverted[key] = true
					uncertain = append(uncertain, fmt.Sprintf("%s: reverted by %s", key, c.SHA))
					continue
				}
				found[key] = append(found[key], evidence{Component: components, Repo: k.repo, SHA: c.SHA})
			}
		}
	}
	// A revert in the range undoes the fix, or an earlier one.
	for key := range reverted {
		delete(found, key)
	}

	now := s.now().UTC()
	var actions []Action
	for _, key := range slices.Sorted(maps.Keys(found)) {
		ev := found[key]
		slices.SortFunc(ev, func(a, b evidence) int {
			return cmp.Or(cmp.Compare(a.Component, b.Component), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.SHA, b.SHA))
		})
		evJSON, err := json.Marshal(slices.Compact(ev))
		if err != nil {
			return nil, "", err
		}
		a := Action{
			Key:          ActionKey{FBCDigest: f.FBCDigest, IssueKey: key, Release: f.Release},
			Assembly:     f.Assembly,
			FBCNVR:       f.FBCNVR,
			EvidenceJSON: string(evJSON),
			UpdatedAt:    now,
		}
		first, ok, err := s.store.FirstIncludedAction(ctx, stream, key)
		if err != nil {
			return nil, "", err
		}
		if ok && first.Key.FBCDigest != f.FBCDigest {
			a.Status = StatusSkipped
			a.SkipReason = fmt.Sprintf("already included in %s (%s)", first.FBCNVR, first.Key.Release)
		} else {
			a.Status = StatusProposed
			a.DesiredTargetVersion = "quay-v" + f.Assembly
		}
		actions = append(actions, a)
	}
	if len(uncertain) > 0 {
		s.uncertain(f, uncertain...)
	}
	return actions, "", nil
}

// awaiting reports whether the ART source of digest may still arrive for f:
// a stored record the source backfill has not checked yet, or no record yet
// within provenanceWait of f's Release completing.
func (s *Sync) awaiting(ctx context.Context, f StageFBC, digest string) (bool, error) {
	stored, unchecked, err := s.store.SourceState(ctx, digest)
	if err != nil || unchecked {
		return unchecked, err
	}
	completed, err := time.Parse(time.RFC3339, f.CompletionTime)
	return !stored && err == nil && s.now().Sub(completed) < provenanceWait, nil
}

// wait leaves f not done because of why, logging each new why once: a
// wait on an unchecked record has no bound.
func (s *Sync) wait(f StageFBC, why string) {
	k := [2]string{f.Release, f.FBCDigest}
	if s.waiting[k] != why {
		s.waiting[k] = why
		s.logger.Info("waiting", "release", f.Release, "fbc", f.FBCNVR, "reason", why)
	}
}

// memberSources returns the ART sources of each member digest of an FBC.
func (s *Sync) memberSources(ctx context.Context, fbcDigest string) (map[string][]Source, error) {
	digests, err := s.store.FBCMemberDigests(ctx, fbcDigest)
	if err != nil {
		return nil, err
	}
	sources := map[string][]Source{}
	for _, d := range digests {
		srcs, err := s.store.SourcesByDigest(ctx, d)
		if err != nil {
			return nil, err
		}
		sources[d] = srcs
	}
	return sources, nil
}

// allowlisted returns the repository and branch of src's allowlisted
// component, or why its commits cannot be trusted.
func allowlisted(src Source, stream string) (repo, branch, reason string) {
	c, ok := allowlist[src.Name]
	switch {
	case !ok:
		return "", "", "not in the allowlist"
	case src.SourceRepo != c.repo:
		return "", "", fmt.Sprintf("source repo %s is not %s", src.SourceRepo, c.repo)
	case !fullSHA.MatchString(src.Commitish):
		return "", "", fmt.Sprintf("commit %q is not a full SHA", src.Commitish)
	}
	b := "redhat-" + stream
	if c.branches != nil {
		b = c.branches[stream]
	}
	if b == "" || !slices.Contains(streams, stream) {
		return "", "", "no branch for stream " + stream
	}
	return c.repo, b, ""
}

// uncertain logs why some of f's issues may not be known.
func (s *Sync) uncertain(f StageFBC, reasons ...string) {
	slices.Sort(reasons)
	s.logger.Warn("uncertain", "release", f.Release, "fbc", cmp.Or(f.FBCNVR, f.FBCDigest), "reasons", strings.Join(slices.Compact(reasons), "; "))
}

// record stores f's actions and marks it done in one transaction, so a
// restart neither repeats nor loses it, then logs what each proposed action
// would write: with the writer off, the log is the only place it shows.
func (s *Sync) record(ctx context.Context, f StageFBC, actions []Action) error {
	err := s.tx(ctx, func(st Store) error {
		for _, a := range actions {
			if err := st.UpsertAction(ctx, a); err != nil {
				return err
			}
		}
		return st.MarkRun(ctx, f.Release, f.FBCDigest)
	})
	if err != nil {
		return err
	}
	for _, a := range actions {
		if a.Status == StatusProposed {
			s.logger.Info("proposed", "issue", a.Key.IssueKey, "fbc", a.FBCNVR, "release", a.Key.Release,
				"target_version_add", a.DesiredTargetVersion, "target_version_remove", placeholder(a.DesiredTargetVersion),
				"transition", "Verified -> "+releasePending+", when Verified", "comment", "FBC stamp")
		}
	}
	return nil
}

func (s *Sync) tx(ctx context.Context, fn func(Store) error) error {
	if s.Tx == nil {
		return fn(s.store)
	}
	return s.Tx(ctx, fn)
}
