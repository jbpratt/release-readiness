package reconcile

import "testing"

func TestBranch(t *testing.T) {
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
		got, ok := branchOf(tc.component, tc.stream)
		if got != tc.want || ok != tc.ok {
			t.Errorf("branchOf(%s, %s) = %q, %v; want %q, %v", tc.component, tc.stream, got, ok, tc.want, tc.ok)
		}
	}
}
