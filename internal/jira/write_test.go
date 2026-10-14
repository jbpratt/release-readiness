package jira

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func newTestWriteClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(Config{BaseURL: srv.URL, Email: "writer@example.com", Token: "write-token", Project: "PROJQUAY"})
	c.minDelay = 0
	return c
}

func TestWriteClientRequests(t *testing.T) {
	type req struct{ method, path, query, body, auth string }
	var got []req
	w := newTestWriteClient(t, func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		got = append(got, req{r.Method, r.URL.Path, r.URL.RawQuery, string(body), user + ":" + pass})
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/2/issue/PROJQUAY-1":
			_, _ = io.WriteString(rw, `{"key":"PROJQUAY-1","fields":{"project":{"key":"PROJQUAY"},"status":{"name":"Verified"},"customfield_10855":[{"name":"quay-v3.18.z"},{"name":"quay-v3.17.z"}]}}`)
		case "GET /rest/api/2/issue/PROJQUAY-1/editmeta":
			_, _ = io.WriteString(rw, `{"fields":{"customfield_10855":{"name":"Target Version"}}}`)
		case "GET /rest/api/2/issue/PROJQUAY-1/transitions":
			_, _ = io.WriteString(rw, `{"transitions":[{"id":"31","name":"Release","to":{"name":"Release Pending"}}]}`)
		case "GET /rest/api/2/issue/PROJQUAY-1/comment":
			if r.URL.Query().Get("startAt") == "0" {
				_, _ = io.WriteString(rw, `{"total":2,"comments":[{"id":"1","body":"a"}]}`)
			} else {
				_, _ = io.WriteString(rw, `{"total":2,"comments":[{"id":"2","body":"b"}]}`)
			}
		case "POST /rest/api/2/issue/PROJQUAY-1/comment":
			rw.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(rw, `{"id":"3","body":"hi"}`)
		default:
			rw.WriteHeader(http.StatusNoContent)
		}
	})
	ctx := t.Context()

	issue, err := w.GetIssue(ctx, "PROJQUAY-1")
	if want := (WriteIssue{Project: "PROJQUAY", Status: "Verified", TargetVersions: []string{"quay-v3.18.z", "quay-v3.17.z"}}); err != nil || !reflect.DeepEqual(issue, want) {
		t.Errorf("GetIssue = %+v, %v; want %+v", issue, err, want)
	}
	if ok, err := w.TargetVersionEditable(ctx, "PROJQUAY-1"); err != nil || !ok {
		t.Errorf("TargetVersionEditable = %v, %v; want true", ok, err)
	}
	if ts, err := w.GetTransitions(ctx, "PROJQUAY-1"); err != nil || !reflect.DeepEqual(ts, []Transition{{ID: "31", To: "Release Pending"}}) {
		t.Errorf("GetTransitions = %+v, %v", ts, err)
	}
	if cs, err := w.ListComments(ctx, "PROJQUAY-1"); err != nil || !reflect.DeepEqual(cs, []Comment{{"a"}, {"b"}}) {
		t.Errorf("ListComments = %+v, %v; want both pages", cs, err)
	}
	if err := w.EditTargetVersions(ctx, "PROJQUAY-1", "quay-v3.18.2", "quay-v3.18.z"); err != nil {
		t.Errorf("EditTargetVersions: %v", err)
	}
	if err := w.Transition(ctx, "PROJQUAY-1", "31"); err != nil {
		t.Errorf("Transition: %v", err)
	}
	if err := w.AddComment(ctx, "PROJQUAY-1", "hi"); err != nil {
		t.Errorf("AddComment: %v", err)
	}

	auth := "writer@example.com:write-token"
	want := []req{
		{"GET", "/rest/api/2/issue/PROJQUAY-1", "fields=project%2Cstatus%2Ccustomfield_10855", "", auth},
		{"GET", "/rest/api/2/issue/PROJQUAY-1/editmeta", "", "", auth},
		{"GET", "/rest/api/2/issue/PROJQUAY-1/transitions", "", "", auth},
		{"GET", "/rest/api/2/issue/PROJQUAY-1/comment", "maxResults=100&startAt=0", "", auth},
		{"GET", "/rest/api/2/issue/PROJQUAY-1/comment", "maxResults=100&startAt=1", "", auth},
		{"PUT", "/rest/api/2/issue/PROJQUAY-1", "", `{"update":{"customfield_10855":[{"add":{"name":"quay-v3.18.2"}},{"remove":{"name":"quay-v3.18.z"}}]}}`, auth},
		{"POST", "/rest/api/2/issue/PROJQUAY-1/transitions", "", `{"transition":{"id":"31"}}`, auth},
		{"POST", "/rest/api/2/issue/PROJQUAY-1/comment", "", `{"body":"hi"}`, auth},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requests:\n%+v\nwant:\n%+v", got, want)
	}
}
