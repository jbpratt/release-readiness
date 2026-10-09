// Package syncstatus tracks the outcome of each background sync so the UI can
// warn when one is failing or has gone stale.
package syncstatus

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// A source is stale after staleAfter intervals without a success, but never
// sooner than minStale, so a slow first pass of a fast loop does not flash.
const (
	staleAfter = 3
	minStale   = 10 * time.Minute
)

// Registry holds the sources being tracked. It is in memory only.
type Registry struct {
	mu      sync.Mutex
	now     func() time.Time
	sources []*Source
}

// Source is one tracked sync loop.
type Source struct {
	r            *Registry
	name         string
	interval     time.Duration
	started      time.Time
	lastSuccess  time.Time
	lastError    string
	failingSince time.Time
}

// Problem is a source whose last pass failed or that has gone stale.
type Problem struct {
	Source      string     `json:"source"`
	Message     string     `json:"message"`
	Since       *time.Time `json:"since"`
	LastSuccess *time.Time `json:"last_success"`
}

func New() *Registry {
	return &Registry{now: time.Now}
}

// Track registers a source that runs every interval. An interval of 0 skips
// the staleness check.
func (r *Registry) Track(source string, interval time.Duration) *Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := &Source{r: r, name: source, interval: interval, started: r.now().UTC()}
	r.sources = append(r.sources, s)
	slices.SortStableFunc(r.sources, func(a, b *Source) int { return strings.Compare(a.name, b.name) })
	return s
}

// Report records the outcome of one pass: nil for success. A nil Source
// (untracked) ignores it.
func (s *Source) Report(err error) {
	if s == nil {
		return
	}
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	now := s.r.now().UTC()
	if err == nil {
		s.lastSuccess = now
		s.failingSince = time.Time{}
		return
	}
	s.lastError = err.Error()
	if s.failingSince.IsZero() {
		s.failingSince = now
	}
}

// Problems lists the failing or stale sources, sorted by source.
func (r *Registry) Problems(now time.Time) []Problem {
	r.mu.Lock()
	defer r.mu.Unlock()
	problems := []Problem{}
	for _, s := range r.sources {
		if p, ok := s.problem(now); ok {
			problems = append(problems, p)
		}
	}
	return problems
}

// Tracks reports whether source is tracked.
func (r *Registry) Tracks(source string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.sources, func(s *Source) bool { return s.name == source })
}

func (s *Source) problem(now time.Time) (Problem, bool) {
	p := Problem{Source: s.name, Since: ptr(s.failingSince), LastSuccess: ptr(s.lastSuccess)}
	if !s.failingSince.IsZero() {
		p.Message = s.lastError
		return p, true
	}
	last := s.lastSuccess
	if last.IsZero() {
		last = s.started
	}
	if limit := max(staleAfter*s.interval, minStale); s.interval > 0 && now.Sub(last) > limit {
		p.Message = fmt.Sprintf("no successful sync in %s", limit)
		return p, true
	}
	return Problem{}, false
}

func ptr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Pass collects the errors of one sync pass into the single outcome Report takes.
type Pass struct {
	first error
	n     int
}

// Add records err if it is non-nil.
func (p *Pass) Add(err error) {
	if err == nil {
		return
	}
	if p.first == nil {
		p.first = err
	}
	p.n++
}

// Err returns nil if nothing failed, else the first error and the count.
func (p *Pass) Err() error {
	switch p.n {
	case 0:
		return nil
	case 1:
		return p.first
	}
	return fmt.Errorf("%w (%d errors this pass)", p.first, p.n)
}
