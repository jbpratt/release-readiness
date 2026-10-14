package jira

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newTestWriteClient(t *testing.T, h http.HandlerFunc) *WriteClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	w := NewWriteClient(Config{BaseURL: srv.URL, Email: "writer@example.com", Token: "write-token", Project: "PROJQUAY"})
	w.c.minDelay = 0
	w.unit = time.Millisecond
	return w
}

func TestWriteClientRetries(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		var calls int
		w := newTestWriteClient(t, func(rw http.ResponseWriter, r *http.Request) {
			calls++
			if calls <= 2 {
				rw.Header().Set("Retry-After", "1")
				rw.WriteHeader(code)
				return
			}
			rw.WriteHeader(http.StatusNoContent)
		})
		if err := w.EditTargetVersions(t.Context(), "PROJQUAY-1", "quay-v3.18.2", "quay-v3.18.z"); err != nil || calls != 3 {
			t.Errorf("PUT %d twice: err %v after %d calls; want success on the 3rd", code, err, calls)
		}
	}

	var calls int
	w := newTestWriteClient(t, func(rw http.ResponseWriter, r *http.Request) {
		calls++
		rw.Header().Set("Retry-After", "1")
		rw.WriteHeader(http.StatusTooManyRequests)
	})
	err := w.Transition(t.Context(), "PROJQUAY-1", "31")
	if err == nil || !strings.Contains(err.Error(), "429 after 3 retries") || calls != 4 {
		t.Errorf("429 always: err %v after %d calls; want an error after 4", err, calls)
	}
}

// A POST that got a 503 may have been made, so it is not retried; a 429 is.
func TestWriteClientPostRetries(t *testing.T) {
	for code, wantCalls := range map[int]int{http.StatusServiceUnavailable: 1, http.StatusTooManyRequests: 2} {
		var calls int
		w := newTestWriteClient(t, func(rw http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				rw.WriteHeader(code)
				return
			}
			rw.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(rw, `{"id":"3"}`)
		})
		_, err := w.AddComment(t.Context(), "PROJQUAY-1", "hi")
		if calls != wantCalls || (err != nil) != (code == http.StatusServiceUnavailable) {
			t.Errorf("POST comment %d: err %v after %d calls; want %d calls", code, err, calls, wantCalls)
		}
	}
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
			_, _ = io.WriteString(rw, `{"key":"PROJQUAY-1","fields":{"project":{"key":"PROJQUAY"},"issuetype":{"name":"Bug"},"status":{"name":"Verified"},"customfield_10855":[{"name":"quay-v3.18.z"},{"name":"quay-v3.17.z"}]}}`)
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
	if want := (WriteIssue{Key: "PROJQUAY-1", Project: "PROJQUAY", Type: "Bug", Status: "Verified", TargetVersions: []string{"quay-v3.18.z", "quay-v3.17.z"}}); err != nil || !reflect.DeepEqual(issue, want) {
		t.Errorf("GetIssue = %+v, %v; want %+v", issue, err, want)
	}
	if meta, err := w.GetEditMeta(ctx, "PROJQUAY-1"); err != nil || !meta.TargetVersion {
		t.Errorf("GetEditMeta = %+v, %v; want Target Version editable", meta, err)
	}
	if ts, err := w.GetTransitions(ctx, "PROJQUAY-1"); err != nil || !reflect.DeepEqual(ts, []Transition{{ID: "31", Name: "Release", To: "Release Pending"}}) {
		t.Errorf("GetTransitions = %+v, %v", ts, err)
	}
	if cs, err := w.ListComments(ctx, "PROJQUAY-1"); err != nil || !reflect.DeepEqual(cs, []Comment{{"1", "a"}, {"2", "b"}}) {
		t.Errorf("ListComments = %+v, %v; want both pages", cs, err)
	}
	if err := w.EditTargetVersions(ctx, "PROJQUAY-1", "quay-v3.18.2", "quay-v3.18.z"); err != nil {
		t.Errorf("EditTargetVersions: %v", err)
	}
	if err := w.Transition(ctx, "PROJQUAY-1", "31"); err != nil {
		t.Errorf("Transition: %v", err)
	}
	if id, err := w.AddComment(ctx, "PROJQUAY-1", "hi"); err != nil || id != "3" {
		t.Errorf("AddComment = %q, %v; want 3", id, err)
	}

	auth := "writer@example.com:write-token"
	want := []req{
		{"GET", "/rest/api/2/issue/PROJQUAY-1", "fields=project%2Cissuetype%2Cstatus%2Ccustomfield_10855", "", auth},
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
