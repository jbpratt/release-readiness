package reconcile

import "testing"

func TestAllowlisted(t *testing.T) {
	for _, tc := range []struct {
		component, repo, stream, branch, reason string
	}{
		{"quay-quay", quayRepo, "3.18", "redhat-3.18", ""},
		{"quay-operator-fbc", "https://github.com/quay/quay-operator", "3.16", "redhat-3.16", ""},
		{"quay-clair", "https://github.com/quay/clair", "3.16", "release-4.8", ""},
		{"quay-clair", "https://github.com/quay/clair", "3.18", "release-4.9", ""},
		{"quay-quay", quayRepo, "5.0", "redhat-5.0", ""},
		{"quay-clair", "https://github.com/quay/clair", "5.0", "release-4.9", ""},
		{"quay-quay", quayRepo, "3.15", "", "no branch for stream 3.15"},
		{"base-rhel9", quayRepo, "3.18", "", "not in the allowlist"},
	} {
		_, branch, reason := allowlisted(Source{Name: tc.component, SourceRepo: tc.repo, Commitish: sha(1)}, tc.stream)
		if branch != tc.branch || reason != tc.reason {
			t.Errorf("allowlisted(%s, %s) = %q, %q; want %q, %q", tc.component, tc.stream, branch, reason, tc.branch, tc.reason)
		}
	}
}
