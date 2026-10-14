package reconcile

import (
	"context"
	"time"
)

// Action statuses.
const (
	StatusProposed  = "proposed"
	StatusSkipped   = "skipped"
	StatusUncertain = "uncertain"
)

// ActionKey identifies an action: an issue an FBC's Release fixes. IssueKey
// is "" for the row listing the FBC's uncertainties.
type ActionKey struct {
	FBCDigest, IssueKey string
	// Release is the Konflux Release name.
	Release string
}

// Action is what the reconciler decided for an issue in an FBC, with the
// commits behind it.
type Action struct {
	Key                                       ActionKey
	Assembly, FBCNVR, BaselineDigest          string
	EvidenceJSON                              string
	DesiredTargetVersion, ActualTargetVersion string
	StatusBefore, StatusAfter, Status         string
	SkipReason, Error                         string
	VersionStep, TransitionStep, CommentStep  string
	CommentMarker, CommentID                  string
	Attempts                                  int
	LastAttemptAt, UpdatedAt                  time.Time
}

// ActionStore stores actions.
type ActionStore interface {
	// UpsertAction stores a, leaving an existing proposed row as it is.
	UpsertAction(context.Context, Action) error
	ListActions(ctx context.Context, assembly string) ([]Action, error)
	// FirstIncludedAction returns the earliest proposed or applied action
	// for issueKey in an assembly of stream.
	FirstIncludedAction(ctx context.Context, stream, issueKey string) (Action, bool, error)
}

// StageFBC is one successful STAGE Release of an FBC Snapshot with a
// rendered catalog.
type StageFBC struct {
	Release, Assembly, CompletionTime, FBCDigest, FBCNVR string
}

// FBCStore reads staged FBCs and records which have been reconciled.
type FBCStore interface {
	// ListStageFBCs returns every StageFBC, oldest completion first.
	ListStageFBCs(ctx context.Context) ([]StageFBC, error)
	// FBCMemberDigests returns the digests of the FBC's head bundle, its
	// related images and the FBC itself.
	FBCMemberDigests(ctx context.Context, fbcDigest string) ([]string, error)
	// RunDone reports whether the FBC of release was reconciled without
	// error.
	RunDone(ctx context.Context, release, fbcDigest string) (bool, error)
	MarkRun(ctx context.Context, release, fbcDigest, errText string) error
}

// Store is the database layer the reconciler needs.
type Store interface {
	ProvenanceStore
	ActionStore
	FBCStore
}
