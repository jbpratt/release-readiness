package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/jira"
)

// JiraWriter reads and writes the Jira issues of actions.
type JiraWriter interface {
	GetIssue(context.Context, string) (jira.WriteIssue, error)
	ListVersions(context.Context) (map[string]jira.VersionField, error)
	GetEditMeta(context.Context, string) (jira.EditMeta, error)
	GetTransitions(context.Context, string) ([]jira.Transition, error)
	EditTargetVersions(ctx context.Context, key, add, remove string) error
	Transition(context.Context, string, string) error
	ListComments(context.Context, string) ([]jira.Comment, error)
	AddComment(context.Context, string, string) (string, error)
}

const (
	releasePending = "Release Pending"
	// maxAttempts is how often an action is tried before it is left in error.
	maxAttempts = 5
)

var errKilled = errors.New("kill file present")

// Writer applies proposed actions to Jira: it narrows the issue's stream
// Target Version to the release, moves a Verified issue to Release Pending
// and comments the FBC that includes the fix.
type Writer struct {
	jira    JiraWriter
	store   ActionStore
	project string
	logger  *slog.Logger
	now     func() time.Time
	// DryRun reads Jira but writes neither Jira nor the actions.
	DryRun bool
	// KillFile, while it exists, stops every Jira write.
	KillFile string
	// dryRan holds the actions a dry run went through: unsaved, they would
	// otherwise be read from Jira again on every pass.
	dryRan map[ActionKey]bool
}

func NewWriter(j JiraWriter, store ActionStore, project string, log *slog.Logger) *Writer {
	return &Writer{jira: j, store: store, project: project, logger: log, now: time.Now}
}

// ApplyPending applies every pending action. Only store errors fail it.
func (w *Writer) ApplyPending(ctx context.Context) error {
	actions, err := w.store.PendingActions(ctx, maxAttempts)
	if err != nil {
		return err
	}
	for _, a := range actions {
		if err := w.Apply(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// Apply edits the Target Version, transitions and comments, in that order,
// re-reading the issue before each. Each step's result is stored as it
// completes, so a retry redoes only the steps not done. A failed Jira call
// leaves a in error; only store errors are returned.
func (w *Writer) Apply(ctx context.Context, a Action) error {
	if a.Key.IssueKey == "" || a.Status != StatusProposed && a.Status != StatusError || w.dryRan[a.Key] {
		return nil
	}
	// Checked before the attempt is counted, so a stop does not use up the
	// attempts of an action in error.
	if w.killed() {
		w.logger.Warn("kill file present, Jira writes stopped", "file", w.KillFile, "issue", a.Key.IssueKey)
		return nil
	}
	a.Attempts++
	a.LastAttemptAt = w.now().UTC()
	for _, step := range []func(context.Context, *Action) error{w.version, w.transition, w.comment} {
		err := step(ctx, &a)
		switch {
		case errors.Is(err, errKilled):
			w.logger.Warn("kill file present, Jira writes stopped", "file", w.KillFile, "issue", a.Key.IssueKey)
			return nil
		case err != nil && ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			w.logger.Error("jira write", "issue", a.Key.IssueKey, "fbc", a.FBCNVR, "error", err)
			a.Status, a.Error = StatusError, err.Error()
			return w.save(ctx, a)
		case a.Status == StatusSkipped:
			w.logger.Info("jira write skipped", "issue", a.Key.IssueKey, "fbc", a.FBCNVR, "reason", a.SkipReason)
			return w.save(ctx, a)
		}
		if err := w.save(ctx, a); err != nil {
			return err
		}
	}
	a.Status, a.Error = StatusApplied, ""
	return w.save(ctx, a)
}

// issue re-reads a's issue, and reports false after skipping a when it is
// not eligible.
func (w *Writer) issue(ctx context.Context, a *Action) (jira.WriteIssue, bool, error) {
	issue, err := w.jira.GetIssue(ctx, a.Key.IssueKey)
	if err != nil {
		return issue, false, err
	}
	if a.StatusBefore == "" {
		a.StatusBefore = issue.Status
	}
	a.StatusAfter = issue.Status
	stream := placeholder(a.DesiredTargetVersion)
	switch {
	case issue.Project != w.project:
		return issue, false, skip(a, fmt.Sprintf("project %s is not %s", issue.Project, w.project))
	case !slices.Contains(issue.TargetVersions, stream) && !slices.Contains(issue.TargetVersions, a.DesiredTargetVersion):
		return issue, false, skip(a, fmt.Sprintf("Target Version %v has neither %s nor %s", issue.TargetVersions, stream, a.DesiredTargetVersion))
	}
	return issue, true, nil
}

// placeholder returns the stream version a release version replaces,
// quay-v3.18.z for quay-v3.18.2.
func placeholder(version string) string {
	return strings.TrimSuffix(version, path.Ext(version)) + ".z"
}

func skip(a *Action, reason string) error {
	a.Status, a.SkipReason, a.Error = StatusSkipped, reason, ""
	return nil
}

func (w *Writer) version(ctx context.Context, a *Action) error {
	if a.VersionStep != "" {
		return nil
	}
	issue, ok, err := w.issue(ctx, a)
	if !ok {
		return err
	}
	versions, err := w.jira.ListVersions(ctx)
	if err != nil {
		return err
	}
	v, found := versions[a.DesiredTargetVersion]
	switch {
	case !found:
		return skip(a, "target version "+a.DesiredTargetVersion+" missing")
	case v.Archived:
		return skip(a, "target version "+a.DesiredTargetVersion+" archived")
	case v.Released:
		return skip(a, "target version "+a.DesiredTargetVersion+" released")
	}
	meta, err := w.jira.GetEditMeta(ctx, a.Key.IssueKey)
	if err != nil {
		return err
	}
	if !meta.TargetVersion {
		return skip(a, "Target Version is not editable")
	}
	a.ActualTargetVersion = a.DesiredTargetVersion
	if slices.Contains(issue.TargetVersions, a.DesiredTargetVersion) {
		a.VersionStep = "already"
		return nil
	}
	stream := placeholder(a.DesiredTargetVersion)
	if err := w.write(a, "replace Target Version "+stream+" with "+a.DesiredTargetVersion, func() error {
		return w.jira.EditTargetVersions(ctx, a.Key.IssueKey, a.DesiredTargetVersion, stream)
	}); err != nil {
		return err
	}
	a.VersionStep = "edited"
	return nil
}

// transition moves a Verified issue to Release Pending. Release-tool makes
// the same move, so an issue already there is left as it is.
func (w *Writer) transition(ctx context.Context, a *Action) error {
	if a.TransitionStep != "" {
		return nil
	}
	issue, ok, err := w.issue(ctx, a)
	if !ok {
		return err
	}
	if issue.Status == releasePending {
		a.TransitionStep = "already"
		return nil
	}
	if issue.Status != "Verified" {
		a.TransitionStep = "none from " + issue.Status
		return nil
	}
	ts, err := w.jira.GetTransitions(ctx, a.Key.IssueKey)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(ts, func(t jira.Transition) bool { return t.To == releasePending })
	if i < 0 {
		a.TransitionStep = "none offered from Verified"
		return nil
	}
	if err := w.write(a, "transition to "+releasePending, func() error {
		return w.jira.Transition(ctx, a.Key.IssueKey, ts[i].ID)
	}); err != nil {
		return err
	}
	a.TransitionStep, a.StatusAfter = "transitioned", releasePending
	return nil
}

// comment says on the issue which FBC includes its fix, once: a comment
// carrying the marker is never posted again.
func (w *Writer) comment(ctx context.Context, a *Action) error {
	if a.CommentStep != "" {
		return nil
	}
	if _, ok, err := w.issue(ctx, a); !ok {
		return err
	}
	a.CommentMarker = fmt.Sprintf("release-readiness FBC stamp %s %s", a.Key.FBCDigest, a.Key.IssueKey)
	comments, err := w.jira.ListComments(ctx, a.Key.IssueKey)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(comments, func(c jira.Comment) bool { return strings.Contains(c.Body, a.CommentMarker) }); i >= 0 {
		a.CommentID, a.CommentStep = comments[i].ID, "already"
		return nil
	}
	var ev []evidence
	if err := json.Unmarshal([]byte(a.EvidenceJSON), &ev); err != nil {
		return fmt.Errorf("evidence: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The fix is included in FBC %s (%s), staged for %s by Konflux Release %s.\n\nCommits:\n", a.FBCNVR, a.Key.FBCDigest, a.DesiredTargetVersion, a.Key.Release)
	for _, e := range ev {
		fmt.Fprintf(&b, "* %s/commit/%s (%s)\n", e.Repo, e.SHA, e.Component)
	}
	fmt.Fprintf(&b, "\n%s\n", a.CommentMarker)
	var id string
	if err := w.write(a, "comment", func() error {
		id, err = w.jira.AddComment(ctx, a.Key.IssueKey, b.String())
		return err
	}); err != nil {
		return err
	}
	a.CommentID, a.CommentStep = id, "added"
	return nil
}

// write makes a Jira write call, unless the kill file exists or this is a
// dry run. A kill file that cannot be checked stops writes too.
func (w *Writer) write(a *Action, what string, call func() error) error {
	if w.killed() {
		return errKilled
	}
	if w.DryRun {
		w.logger.Info("dry run, would "+what, "issue", a.Key.IssueKey, "fbc", a.FBCNVR)
		return nil
	}
	return call()
}

func (w *Writer) killed() bool {
	if w.KillFile == "" {
		return false
	}
	_, err := os.Stat(w.KillFile)
	return !errors.Is(err, fs.ErrNotExist)
}

func (w *Writer) save(ctx context.Context, a Action) error {
	if w.DryRun {
		// A failed action is gone through too, so a failing read is tried
		// and logged once per process.
		if w.dryRan == nil {
			w.dryRan = map[ActionKey]bool{}
		}
		w.dryRan[a.Key] = true
		return nil
	}
	return w.store.UpsertAction(ctx, a)
}
