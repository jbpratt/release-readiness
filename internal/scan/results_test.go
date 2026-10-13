package scan

import (
	"net/http/httptest"
	"testing"
	"time"
)

// records follows next_page_token to the last page.
func TestRecordsPages(t *testing.T) {
	svc := newService(t, "quay-3-18-quay-operator-7mbwk")
	svc.maxPage = 3
	srv := httptest.NewServer(svc)
	defer srv.Close()
	pace := time.NewTicker(time.Millisecond)
	defer pace.Stop()
	runs, err := NewClient(srv.URL, srv.Client()).records(t.Context(), pace.C, "art-quay-tenant",
		`data_type == "tekton.dev/v1.TaskRun" && data.metadata.labels["tekton.dev/pipelineRun"] == "quay-3-18-quay-operator-7mbwk" && data.metadata.labels["tekton.dev/pipelineTask"] == "roxctl-scan"`)
	if err != nil || len(runs) != 4 || svc.results != 2 {
		t.Fatalf("records: %d runs in %d requests, %v; want 4 in 2", len(runs), svc.results, err)
	}
}
