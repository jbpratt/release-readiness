// Command stub is a stdlib-only HTTP server that serves canned Konflux
// Snapshot and JIRA REST responses from fixture files, for the local e2e
// harness (dev/e2e.sh). It never talks to a real cluster or JIRA instance.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8089", "listen address")
	fixtures := flag.String("fixtures", "dev/e2e/fixtures", "path to fixtures directory")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /apis/appstudio.redhat.com/v1alpha1/namespaces/{namespace}/snapshots",
		serveFixture(*fixtures, "konflux-snapshots.json"))
	mux.HandleFunc("GET /rest/api/3/search/jql", handleSearchJQL(*fixtures))
	mux.HandleFunc("GET /rest/api/3/project/{key}/versions",
		serveFixture(*fixtures, "jira-versions.json"))

	log.Printf("e2e stub listening on %s (fixtures: %s)", *addr, *fixtures)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func serveFixture(fixturesDir, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, fixturesDir, name)
	}
}

// handleSearchJQL dispatches JIRA's /rest/api/3/search/jql to one of two
// fixtures based on the jql query: the release-discovery query (filters on
// the "-area/release" component) or a per-version issue search.
func handleSearchJQL(fixturesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		if strings.Contains(jql, "area/release") {
			writeFixture(w, fixturesDir, "jira-release-search.json")
			return
		}
		writeFixture(w, fixturesDir, "jira-issue-search.json")
	}
}

func writeFixture(w http.ResponseWriter, fixturesDir, name string) {
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		http.Error(w, fmt.Sprintf("read fixture %s: %v", name, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
