package reconcile

import (
	"strings"
	"testing"
)

func TestBranch(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		component, stream, want string
		ok                      bool
	}{
		{"quay-quay", "3.18", "redhat-3.18", true},
		{"quay-operator-fbc", "3.16", "redhat-3.16", true},
		{"quay-clair", "3.16", "release-4.8", true},
		{"quay-clair", "3.18", "release-4.9", true},
		{"quay-quay", "3.15", "", false},
		{"base-rhel9", "3.18", "", false},
	} {
		got, ok := c.Branch(tc.component, tc.stream)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Branch(%s, %s) = %q, %v; want %q, %v", tc.component, tc.stream, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, tc := range []struct{ name, json, err string }{
		{"no streams", `{"components":[]}`, "no streams"},
		{"not github", `{"streams":["3.18"],"components":[{"name":"a","sourceRepo":"https://gitlab.com/quay/a","branch":"main"}]}`, "not on https://github.com/"},
		{"bare github", `{"streams":["3.18"],"components":[{"name":"a","sourceRepo":"https://github.com/","branch":"main"}]}`, "not on https://github.com/"},
		{"both branches", `{"streams":["3.18"],"components":[{"name":"a","sourceRepo":"https://github.com/quay/a","branch":"main","branches":{"3.18":"x"}}]}`, "exactly one"},
		{"no branch", `{"streams":["3.18"],"components":[{"name":"a","sourceRepo":"https://github.com/quay/a"}]}`, "exactly one"},
		{"bad baseline", `{"streams":["3.18"],"baseline":{"selection":"latest","scope":"stream-operator-ocp-slot","persist":true},"components":[]}`, "unsupported baseline"},
	} {
		if _, err := Parse([]byte(tc.json)); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.err)
		}
	}
}
