// Package reconcile matches the commits an FBC ships to the Jira issues they
// fix.
package reconcile

// streams are the Quay streams whose FBCs are reconciled.
var streams = []string{"3.16", "3.17", "3.18", "5.0"}

// component is an allowlisted ART build's public repository, and the branch
// of each stream its commits land on; nil branches is redhat-{stream}.
type component struct {
	repo     string
	branches map[string]string
}

// allowlist holds the ART builds, by ART name, whose source commits are
// reconciled. Anything it does not list is uncertain.
var allowlist = map[string]component{
	"quay-quay":                               {repo: "https://github.com/quay/quay"},
	"quay-clair":                              {repo: "https://github.com/quay/clair", branches: map[string]string{"3.16": "release-4.8", "3.17": "release-4.9", "3.18": "release-4.9", "5.0": "release-4.9"}},
	"quay-builder":                            {repo: "https://github.com/quay/quay-builder"},
	"quay-builder-qemu":                       {repo: "https://github.com/quay/quay-builder-qemu"},
	"quay-operator":                           {repo: "https://github.com/quay/quay-operator"},
	"quay-operator-bundle":                    {repo: "https://github.com/quay/quay-operator"},
	"quay-operator-fbc":                       {repo: "https://github.com/quay/quay-operator"},
	"quay-bridge-operator":                    {repo: "https://github.com/quay/quay-bridge-operator"},
	"quay-bridge-operator-bundle":             {repo: "https://github.com/quay/quay-bridge-operator"},
	"quay-bridge-operator-fbc":                {repo: "https://github.com/quay/quay-bridge-operator"},
	"container-security-operator":             {repo: "https://github.com/quay/container-security-operator"},
	"quay-container-security-operator-bundle": {repo: "https://github.com/quay/container-security-operator"},
	"container-security-operator-fbc":         {repo: "https://github.com/quay/container-security-operator"},
}
