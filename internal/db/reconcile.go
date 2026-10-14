package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/reconcile"
)

var _ reconcile.Store = (*DB)(nil)

// UpsertSource stores s's name and source repo for its stored ART record.
func (d *DB) UpsertSource(ctx context.Context, s reconcile.Source) error {
	return d.UpsertArtSourceRepo(ctx, s.RecordID, s.Name, s.SourceRepo)
}

func (d *DB) SourceByRecord(ctx context.Context, recordID string) (reconcile.Source, bool, error) {
	r, err := d.queries().GetSourceByRecord(ctx, recordID)
	if errors.Is(err, sql.ErrNoRows) {
		return reconcile.Source{}, false, nil
	}
	if err != nil {
		return reconcile.Source{}, false, err
	}
	return reconcile.Source{RecordID: r.RecordID, Name: r.Name, NVR: r.Nvr, Digest: r.Digest, SourceRepo: r.SourceRepo, Commitish: r.Commitish}, true, nil
}

func (d *DB) SourcesByDigest(ctx context.Context, digest string) ([]reconcile.Source, error) {
	rows, err := d.queries().ListSourcesByDigest(ctx, digest)
	if err != nil {
		return nil, err
	}
	sources := make([]reconcile.Source, len(rows))
	for i, r := range rows {
		sources[i] = reconcile.Source{RecordID: r.RecordID, Name: r.Name, NVR: r.Nvr, Digest: r.Digest, SourceRepo: r.SourceRepo, Commitish: r.Commitish}
	}
	return sources, nil
}

func (d *DB) SourceState(ctx context.Context, digest string) (stored, unchecked bool, err error) {
	r, err := d.queries().GetDigestSourceState(ctx, digest)
	return r.HasRecord, r.HasUnchecked, err
}

func (d *DB) GetBaseline(ctx context.Context, stream, operator, ocp string) (reconcile.Baseline, bool, error) {
	r, err := d.queries().GetReconcileBaseline(ctx, dbsqlc.GetReconcileBaselineParams{Stream: stream, Operator: operator, Ocp: ocp})
	if errors.Is(err, sql.ErrNoRows) {
		return reconcile.Baseline{}, false, nil
	}
	if err != nil {
		return reconcile.Baseline{}, false, err
	}
	return reconcile.Baseline{Stream: r.Stream, Operator: r.Operator, OCP: r.Ocp, FBCDigest: r.FbcDigest, FBCNVR: r.FbcNvr}, true, nil
}

func (d *DB) InsertBaseline(ctx context.Context, b reconcile.Baseline) error {
	return d.queries().InsertReconcileBaseline(ctx, dbsqlc.InsertReconcileBaselineParams{
		Stream:    b.Stream,
		Operator:  b.Operator,
		Ocp:       b.OCP,
		FbcDigest: b.FBCDigest,
		FbcNvr:    b.FBCNVR,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// actionTime is fixed width, so updated_at orders as text.
const actionTime = "2006-01-02T15:04:05.000000000Z"

func formatActionTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(actionTime)
}

func (d *DB) UpsertAction(ctx context.Context, a reconcile.Action) error {
	return d.queries().UpsertReconcileAction(ctx, dbsqlc.UpsertReconcileActionParams{
		FbcDigest:            a.Key.FBCDigest,
		IssueKey:             a.Key.IssueKey,
		ReleaseName:          a.Key.Release,
		Assembly:             a.Assembly,
		FbcNvr:               a.FBCNVR,
		BaselineDigest:       a.BaselineDigest,
		EvidenceJson:         a.EvidenceJSON,
		DesiredTargetVersion: a.DesiredTargetVersion,
		ActualTargetVersion:  a.ActualTargetVersion,
		StatusBefore:         a.StatusBefore,
		StatusAfter:          a.StatusAfter,
		Status:               a.Status,
		SkipReason:           a.SkipReason,
		Error:                a.Error,
		VersionStep:          a.VersionStep,
		TransitionStep:       a.TransitionStep,
		CommentStep:          a.CommentStep,
		CommentMarker:        a.CommentMarker,
		CommentID:            a.CommentID,
		Attempts:             int64(a.Attempts),
		LastAttemptAt:        formatActionTime(a.LastAttemptAt),
		UpdatedAt:            formatActionTime(a.UpdatedAt),
	})
}

func (d *DB) PendingActions(ctx context.Context, maxAttempts int) ([]reconcile.Action, error) {
	rows, err := d.queries().ListPendingReconcileActions(ctx, int64(maxAttempts))
	if err != nil {
		return nil, err
	}
	actions := make([]reconcile.Action, len(rows))
	for i, r := range rows {
		actions[i] = toAction(r)
	}
	return actions, nil
}

func (d *DB) FirstIncludedAction(ctx context.Context, stream, issueKey string) (reconcile.Action, bool, error) {
	r, err := d.queries().FirstIncludedReconcileAction(ctx, dbsqlc.FirstIncludedReconcileActionParams{IssueKey: issueKey, Prefix: stream + "."})
	if errors.Is(err, sql.ErrNoRows) {
		return reconcile.Action{}, false, nil
	}
	if err != nil {
		return reconcile.Action{}, false, err
	}
	return toAction(r), true, nil
}

func toAction(r dbsqlc.ReconcileAction) reconcile.Action {
	return reconcile.Action{
		Key:                  reconcile.ActionKey{FBCDigest: r.FbcDigest, IssueKey: r.IssueKey, Release: r.ReleaseName},
		Assembly:             r.Assembly,
		FBCNVR:               r.FbcNvr,
		BaselineDigest:       r.BaselineDigest,
		EvidenceJSON:         r.EvidenceJson,
		DesiredTargetVersion: r.DesiredTargetVersion,
		ActualTargetVersion:  r.ActualTargetVersion,
		StatusBefore:         r.StatusBefore,
		StatusAfter:          r.StatusAfter,
		Status:               r.Status,
		SkipReason:           r.SkipReason,
		Error:                r.Error,
		VersionStep:          r.VersionStep,
		TransitionStep:       r.TransitionStep,
		CommentStep:          r.CommentStep,
		CommentMarker:        r.CommentMarker,
		CommentID:            r.CommentID,
		Attempts:             int(r.Attempts),
		LastAttemptAt:        parseTime(r.LastAttemptAt),
		UpdatedAt:            parseTime(r.UpdatedAt),
	}
}

func (d *DB) ListStageFBCs(ctx context.Context) ([]reconcile.StageFBC, error) {
	rows, err := d.queries().ListStageFBCs(ctx)
	if err != nil {
		return nil, err
	}
	fbcs := make([]reconcile.StageFBC, len(rows))
	for i, r := range rows {
		fbcs[i] = reconcile.StageFBC{Release: r.ReleaseName, Assembly: r.Assembly, CompletionTime: r.CompletionTime, FBCDigest: r.FbcDigest, FBCNVR: r.FbcNvr, RenderState: r.RenderState}
	}
	return fbcs, nil
}

func (d *DB) FBCMemberDigests(ctx context.Context, fbcDigest string) ([]string, error) {
	digests, err := d.queries().ListFBCMemberDigests(ctx, fbcDigest)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(digests, fbcDigest) {
		digests = append(digests, fbcDigest)
	}
	return digests, nil
}

func (d *DB) RunDone(ctx context.Context, release, fbcDigest string) (bool, error) {
	return d.queries().ReconcileRunDone(ctx, dbsqlc.ReconcileRunDoneParams{ReleaseName: release, FbcDigest: fbcDigest})
}

func (d *DB) MarkRun(ctx context.Context, release, fbcDigest, errText string) error {
	return d.queries().UpsertReconcileRun(ctx, dbsqlc.UpsertReconcileRunParams{
		ReleaseName: release,
		FbcDigest:   fbcDigest,
		Error:       errText,
		DoneAt:      time.Now().UTC().Format(time.RFC3339),
	})
}
