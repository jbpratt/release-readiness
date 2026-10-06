package kube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const listPath = "/apis/example.com/v1/namespaces/ns/widgets"

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewClient(&Config{Server: srv.URL, BearerToken: "fake-token", InsecureSkipTLSVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestList_Pagination(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != listPath || r.Header.Get("Authorization") != "Bearer fake-token" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("continue") == "tok1" {
			_, _ = w.Write([]byte(`{"metadata": {}, "items": [{"name": "c"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"metadata": {"continue": "tok1"}, "items": [{"name": "a"}, {"name": "b"}]}`))
	})

	var names []string
	err := client.List(context.Background(), listPath, func(items json.RawMessage) error {
		var page []struct{ Name string }
		if err := json.Unmarshal(items, &page); err != nil {
			return err
		}
		for _, p := range page {
			names = append(names, p.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "a,b,c" {
		t.Errorf("names = %q, want a,b,c", got)
	}
}

func TestList_ErrorStatus(t *testing.T) {
	tests := []struct {
		status  int
		wantErr error
		wantMsg string
	}{
		{http.StatusUnauthorized, ErrUnauthorized, "token expired"},
		{http.StatusForbidden, ErrForbidden, "forbidden"},
		{http.StatusInternalServerError, nil, "unexpected status 500"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "echo: "+r.Header.Get("Authorization"), tt.status)
			})

			err := client.List(context.Background(), listPath, func(json.RawMessage) error { return nil })
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
			if strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "echo") {
				t.Errorf("error = %q leaks the response body or token", err)
			}
		})
	}
}
