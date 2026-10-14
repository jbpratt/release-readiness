package reconcile

import (
	"context"
	"time"
)

// Action statuses.
const (
	StatusProposed = "proposed"
	StatusSkipped  = "skipped"
	StatusApplied  = "applied"
	StatusError    = "error"
)

// ActionKey identifies an action: an issue an FBC's Release fixes.
type ActionKey struct {
	FBCDigest, IssueKey string
	// Release is the Konflux Release name.
	Release string
}

// Action is what the reconciler decided for an issue in an FBC, with the
// commits behind it.
type Action struct {
	Key                                      ActionKey
	Assembly, FBCNVR, EvidenceJSON           string
	DesiredTargetVersion, Status             string
	SkipReason, Error                        string
	VersionStep, TransitionStep, CommentStep string
	Attempts                                 int
	UpdatedAt                                time.Time
}

// StageFBC is one successful STAGE Release of an FBC Snapshot's catalog.
type StageFBC struct {
	Release, Assembly, CompletionTime, FBCDigest, FBCNVR string
	// RenderState is the catalog's render state, "" before its first render.
	RenderState string
}

// Source is an ART build, by its ART name, with the public repository and
// commit it was built from.
type Source struct {
	Name, SourceRepo, Commitish string
}

// Baseline is the first FBC a stream/operator/OCP slot was reconciled from.
type Baseline struct {
	Stream, Operator, OCP, FBCDigest string
}

// Store is the database layer the reconciler and writer need.
type Store interface {
	// ListStageFBCs returns every StageFBC, oldest completion first.
	ListStageFBCs(ctx context.Context) ([]StageFBC, error)
	// FBCMemberDigests returns the digests of the FBC's head bundle, its
	// related images and the FBC itself.
	FBCMemberDigests(ctx context.Context, fbcDigest string) ([]string, error)
	// SourcesByDigest returns the records of an image digest that have a
	// source repo.
	SourcesByDigest(context.Context, string) ([]Source, error)
	// SourceState reports whether an image digest has stored ART records,
	// and whether one of them is not checked for a source repo yet.
	SourceState(ctx context.Context, digest string) (stored, unchecked bool, err error)
	// GetBaseline returns the FBC digest of a slot's baseline.
	GetBaseline(ctx context.Context, stream, operator, ocp string) (string, bool, error)
	// InsertBaseline stores b unless its slot has a baseline already.
	InsertBaseline(context.Context, Baseline) error
	// RunDone reports whether the FBC of release was reconciled.
	RunDone(ctx context.Context, release, fbcDigest string) (bool, error)
	MarkRun(ctx context.Context, release, fbcDigest string) error
	// UpsertAction stores a. Without attempts, a leaves an existing
	// proposed or attempted row as it is; with them, a replaces only a
	// proposed or error row, from no later attempt.
	UpsertAction(context.Context, Action) error
	// PendingActions returns the actions still to write: proposed, or
	// error after fewer than maxAttempts attempts.
	PendingActions(ctx context.Context, maxAttempts int) ([]Action, error)
	// FirstIncludedAction returns the earliest proposed, applied or error
	// action for issueKey in an assembly of stream.
	FirstIncludedAction(ctx context.Context, stream, issueKey string) (Action, bool, error)
}
