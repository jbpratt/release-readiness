package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/quay/release-readiness/internal/jira"
)

// JiraWriter reads and writes the Jira issues of actions.
type JiraWriter interface {
	GetIssue(context.Context, string) (jira.WriteIssue, error)
	ListVersions(context.Context) (map[string]jira.VersionField, error)
	TargetVersionEditable(context.Context, string) (bool, error)
	GetTransitions(context.Context, string) ([]jira.Transition, error)
	EditTargetVersions(ctx context.Context, key, add, remove string) error
	Transition(context.Context, string, string) error
	ListComments(context.Context, string) ([]jira.Comment, error)
	AddComment(context.Context, string, string) error
}

const (
	releasePending = "Release Pending"
	// maxAttempts is how often an action is tried before it is left in error.
	maxAttempts = 5
)

// Writer applies proposed actions to Jira: it narrows the issue's stream
// Target Version to the release, moves a Verified issue to Release Pending
// and comments the FBC that includes the fix.
type Writer struct {
	jira    JiraWriter
	store   Store
	project string
	logger  *slog.Logger
}

func NewWriter(j JiraWriter, store Store, project string, log *slog.Logger) *Writer {
	return &Writer{jira: j, store: store, project: project, logger: log}
}

// ApplyPending applies every pending action. Only store errors fail it.
func (w *Writer) ApplyPending(ctx context.Context) error {
	actions, err := w.store.PendingActions(ctx, maxAttempts)
	if err != nil {
		return err
	}
	for _, a := range actions {
		if err := w.apply(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// apply edits the Target Version, transitions and comments, in that order,
// re-reading the issue before each. Each step's result is stored as it
// completes, so a retry redoes only the steps not done. A failed Jira call
// leaves a in error; only store errors are returned.
func (w *Writer) apply(ctx context.Context, a Action) error {
	a.Attempts++
	for _, step := range []func(context.Context, *Action) error{w.version, w.transition, w.comment} {
		err := step(ctx, &a)
		switch {
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
	editable, err := w.jira.TargetVersionEditable(ctx, a.Key.IssueKey)
	if err != nil {
		return err
	}
	if !editable {
		return skip(a, "Target Version is not editable")
	}
	if slices.Contains(issue.TargetVersions, a.DesiredTargetVersion) {
		a.VersionStep = "already"
		return nil
	}
	if err := w.jira.EditTargetVersions(ctx, a.Key.IssueKey, a.DesiredTargetVersion, placeholder(a.DesiredTargetVersion)); err != nil {
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
	if err := w.jira.Transition(ctx, a.Key.IssueKey, ts[i].ID); err != nil {
		return err
	}
	a.TransitionStep = "transitioned"
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
	marker := fmt.Sprintf("release-readiness FBC stamp %s %s", a.Key.FBCDigest, a.Key.IssueKey)
	comments, err := w.jira.ListComments(ctx, a.Key.IssueKey)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(comments, func(c jira.Comment) bool { return strings.Contains(c.Body, marker) }) {
		a.CommentStep = "already"
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
	fmt.Fprintf(&b, "\n%s\n", marker)
	if err := w.jira.AddComment(ctx, a.Key.IssueKey, b.String()); err != nil {
		return err
	}
	a.CommentStep = "added"
	return nil
}

func (w *Writer) save(ctx context.Context, a Action) error {
	return w.store.UpsertAction(ctx, a)
}
