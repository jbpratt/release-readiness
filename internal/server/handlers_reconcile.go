package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type fbcJiraAction struct {
	FBCDigest            string          `json:"fbc_digest"`
	FBCNVR               string          `json:"fbc_nvr"`
	Release              string          `json:"release"`
	IssueKey             string          `json:"issue_key"`
	Status               string          `json:"status"`
	DesiredTargetVersion string          `json:"desired_target_version"`
	ActualTargetVersion  string          `json:"actual_target_version"`
	SkipReason           string          `json:"skip_reason"`
	Error                string          `json:"error"`
	Evidence             json.RawMessage `json:"evidence"`
	StatusBefore         string          `json:"status_before"`
	StatusAfter          string          `json:"status_after"`
	VersionStep          string          `json:"version_step"`
	TransitionStep       string          `json:"transition_step"`
	CommentStep          string          `json:"comment_step"`
	CommentID            string          `json:"comment_id"`
	Attempts             int             `json:"attempts"`
	LastAttemptAt        *time.Time      `json:"last_attempt_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// handleListFBCJiraActions returns what the reconciler decided for the
// version's staged FBCs.
func (s *Server) handleListFBCJiraActions(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	m := quayVersion.FindStringSubmatch(version)
	if m == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release %q not found", version))
		return
	}
	actions, err := s.db.ListActions(r.Context(), m[1])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]fbcJiraAction, len(actions))
	for i, a := range actions {
		out[i] = fbcJiraAction{
			FBCDigest:            a.Key.FBCDigest,
			FBCNVR:               a.FBCNVR,
			Release:              a.Key.Release,
			IssueKey:             a.Key.IssueKey,
			Status:               a.Status,
			DesiredTargetVersion: a.DesiredTargetVersion,
			ActualTargetVersion:  a.ActualTargetVersion,
			SkipReason:           a.SkipReason,
			Error:                a.Error,
			StatusBefore:         a.StatusBefore,
			StatusAfter:          a.StatusAfter,
			VersionStep:          a.VersionStep,
			TransitionStep:       a.TransitionStep,
			CommentStep:          a.CommentStep,
			CommentID:            a.CommentID,
			Attempts:             a.Attempts,
			UpdatedAt:            a.UpdatedAt,
		}
		// An uncertain row has no evidence.
		if a.EvidenceJSON != "" {
			out[i].Evidence = json.RawMessage(a.EvidenceJSON)
		}
		if !a.LastAttemptAt.IsZero() {
			out[i].LastAttemptAt = &a.LastAttemptAt
		}
	}
	writeJSON(w, http.StatusOK, out)
}
