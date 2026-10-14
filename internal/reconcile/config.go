// Package reconcile matches the commits an FBC ships to the Jira issues they
// fix.
package reconcile

import (
	"context"
	"slices"
)

// streams are the Quay streams whose FBCs are reconciled.
var streams = []string{"3.16", "3.17", "3.18"}

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
	"quay-clair":                              {repo: "https://github.com/quay/clair", branches: map[string]string{"3.16": "release-4.8", "3.17": "release-4.9", "3.18": "release-4.9"}},
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

// branchOf returns the branch name's commits for stream land on, false when
// the allowlist does not list both.
func branchOf(name, stream string) (string, bool) {
	c, ok := allowlist[name]
	if !ok || !slices.Contains(streams, stream) {
		return "", false
	}
	if c.branches == nil {
		return "redhat-" + stream, true
	}
	b, ok := c.branches[stream]
	return b, ok
}

// Source is an ART build record with the public repository and commit it
// was built from.
type Source struct {
	RecordID, Name, NVR, Digest, SourceRepo, Commitish string
}

// Baseline is the first FBC a stream/operator/OCP slot was reconciled from.
type Baseline struct {
	Stream, Operator, OCP, FBCDigest, FBCNVR string
}

// ProvenanceStore stores ART source provenance and slot baselines.
type ProvenanceStore interface {
	UpsertSource(context.Context, Source) error
	// SourceByRecord returns false for a record with no known source repo.
	SourceByRecord(context.Context, string) (Source, bool, error)
	// SourcesByDigest returns the records of an image digest that have a
	// source repo.
	SourcesByDigest(context.Context, string) ([]Source, error)
	// SourceState reports whether an image digest has stored ART records,
	// and whether one of them is not checked for a source repo yet.
	SourceState(ctx context.Context, digest string) (stored, unchecked bool, err error)
	GetBaseline(ctx context.Context, stream, operator, ocp string) (Baseline, bool, error)
	// InsertBaseline stores b unless its slot has a baseline already.
	InsertBaseline(context.Context, Baseline) error
}
