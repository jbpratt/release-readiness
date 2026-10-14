package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/reconcile"
)

func TestListFBCJiraActions(t *testing.T) {
	srv := setupTestServer(t)
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	ev := `[{"component":"quay-quay","repo":"https://github.com/quay/quay","sha":"abc","url":"https://github.com/quay/quay/commit/abc"}]`
	for _, a := range []reconcile.Action{
		{Key: reconcile.ActionKey{FBCDigest: "sha256:f1", IssueKey: "PROJQUAY-1", Release: "r1"}, Assembly: "3.18.2", Status: reconcile.StatusProposed, DesiredTargetVersion: "quay-v3.18.2", EvidenceJSON: ev, UpdatedAt: at},
		{Key: reconcile.ActionKey{FBCDigest: "sha256:f1", IssueKey: "PROJQUAY-2", Release: "r1"}, Assembly: "3.18.2", Status: reconcile.StatusSkipped, SkipReason: "already included", EvidenceJSON: ev, UpdatedAt: at},
		{Key: reconcile.ActionKey{FBCDigest: "sha256:f1", Release: "r1"}, Assembly: "3.18.2", Status: reconcile.StatusUncertain, SkipReason: "component x: no ART source", UpdatedAt: at},
		{Key: reconcile.ActionKey{FBCDigest: "sha256:f0", IssueKey: "PROJQUAY-3", Release: "r0"}, Assembly: "3.18.1", Status: reconcile.StatusProposed, EvidenceJSON: "[]", UpdatedAt: at},
	} {
		if err := srv.db.UpsertAction(t.Context(), a); err != nil {
			t.Fatal(err)
		}
	}

	var got []fbcJiraAction
	getJSON(t, srv, "/api/v1/releases/quay-v3.18.2/fbc-jira-actions", http.StatusOK, &got)
	if len(got) != 3 {
		t.Fatalf("got %d actions, want 3: %+v", len(got), got)
	}
	statuses := map[string]fbcJiraAction{}
	for _, a := range got {
		statuses[a.Status] = a
	}
	if p := statuses[reconcile.StatusProposed]; p.IssueKey != "PROJQUAY-1" || p.DesiredTargetVersion != "quay-v3.18.2" || string(p.Evidence) != ev {
		t.Errorf("proposed = %+v", p)
	}
	if u := statuses[reconcile.StatusUncertain]; u.IssueKey != "" || u.SkipReason != "component x: no ART source" || string(u.Evidence) != "null" {
		t.Errorf("uncertain = %+v", u)
	}

	var raw []json.RawMessage
	getJSON(t, srv, "/api/v1/releases/quay-v3.19.0/fbc-jira-actions", http.StatusOK, &raw)
	if raw == nil || len(raw) != 0 {
		t.Errorf("no rows: got %v, want []", raw)
	}
	getJSON(t, srv, "/api/v1/releases/3.18.2/fbc-jira-actions", http.StatusNotFound, nil)
}
