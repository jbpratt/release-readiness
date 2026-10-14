// Package reconcile matches the commits an FBC ships to the Jira issues they
// fix.
package reconcile

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

//go:embed sources.json
var sourcesJSON []byte

// Config is the allowlist of streams and ART components whose source commits
// are reconciled. Anything it does not list is uncertain.
type Config struct {
	Streams    []string    `json:"streams"`
	Components []Component `json:"components"`
}

// Component is an ART build, by its ART name, with the public repository
// and branch its commits come from. Branch may hold {stream}; Branches maps
// a stream to its branch.
type Component struct {
	Name       string            `json:"name"`
	SourceRepo string            `json:"sourceRepo"`
	Branch     string            `json:"branch"`
	Branches   map[string]string `json:"branches"`
}

// Load returns the embedded allowlist.
func Load() (Config, error) {
	return Parse(sourcesJSON)
}

// Parse decodes and validates an allowlist.
func Parse(data []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	if len(c.Streams) == 0 {
		return Config{}, errors.New("no streams")
	}
	for _, comp := range c.Components {
		if rest, ok := strings.CutPrefix(comp.SourceRepo, "https://github.com/"); !ok || rest == "" {
			return Config{}, fmt.Errorf("component %q: source repo %q is not on https://github.com/", comp.Name, comp.SourceRepo)
		}
		if (comp.Branch == "") == (len(comp.Branches) == 0) {
			return Config{}, fmt.Errorf("component %q: needs exactly one of branch and branches", comp.Name)
		}
	}
	return c, nil
}

// Branch returns the branch component's commits for stream land on, false
// when the allowlist does not list both.
func (c Config) Branch(component, stream string) (string, bool) {
	if !slices.Contains(c.Streams, stream) {
		return "", false
	}
	i := slices.IndexFunc(c.Components, func(comp Component) bool { return comp.Name == component })
	if i < 0 {
		return "", false
	}
	comp := c.Components[i]
	if comp.Branch != "" {
		return strings.ReplaceAll(comp.Branch, "{stream}", stream), true
	}
	b, ok := comp.Branches[stream]
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
	GetBaseline(ctx context.Context, stream, operator, ocp string) (Baseline, bool, error)
	// InsertBaseline stores b unless its slot has a baseline already.
	InsertBaseline(context.Context, Baseline) error
}
