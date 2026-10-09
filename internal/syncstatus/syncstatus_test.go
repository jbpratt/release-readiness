package syncstatus

import (
	"errors"
	"testing"
	"time"
)

func newTestRegistry(now *time.Time) *Registry {
	r := New()
	r.now = func() time.Time { return *now }
	return r
}

func TestFailureStreak(t *testing.T) {
	t0 := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := t0
	r := newTestRegistry(&now)
	s := r.Track("jira", time.Minute)

	s.Report(errors.New("first"))
	now = t0.Add(time.Minute)
	s.Report(errors.New("second"))

	p := r.Problems(now)
	if len(p) != 1 {
		t.Fatalf("problems = %+v, want one", p)
	}
	if p[0].Message != "second" || !p[0].Since.Equal(t0) || p[0].LastSuccess != nil {
		t.Errorf("problem = %+v, want message second since %v", p[0], t0)
	}

	now = t0.Add(2 * time.Minute)
	s.Report(nil)
	if p := r.Problems(now); len(p) != 0 {
		t.Errorf("problems after success = %+v, want none", p)
	}

	now = t0.Add(3 * time.Minute)
	s.Report(errors.New("third"))
	if p := r.Problems(now); len(p) != 1 || !p[0].Since.Equal(now) {
		t.Errorf("problems = %+v, want a new streak since %v", p, now)
	}
}

func TestStale(t *testing.T) {
	t0 := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := t0
	r := newTestRegistry(&now)
	s := r.Track("art-builds", 10*time.Minute)

	if p := r.Problems(t0.Add(30 * time.Minute)); len(p) != 0 {
		t.Errorf("problems at 3x interval = %+v, want none", p)
	}
	if p := r.Problems(t0.Add(30*time.Minute + time.Second)); len(p) != 1 || p[0].Since != nil {
		t.Errorf("problems past 3x interval since start = %+v, want stale", p)
	}

	now = t0.Add(time.Hour)
	s.Report(nil)
	if p := r.Problems(now.Add(30 * time.Minute)); len(p) != 0 {
		t.Errorf("problems within 3x of last success = %+v, want none", p)
	}
	if p := r.Problems(now.Add(31 * time.Minute)); len(p) != 1 || !p[0].LastSuccess.Equal(now) {
		t.Errorf("problems past 3x of last success = %+v, want stale", p)
	}
}

func TestStaleFloor(t *testing.T) {
	t0 := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := t0
	r := newTestRegistry(&now)
	r.Track("konflux", 30*time.Second)

	if p := r.Problems(t0.Add(10 * time.Minute)); len(p) != 0 {
		t.Errorf("problems at the 10m floor = %+v, want none", p)
	}
	if p := r.Problems(t0.Add(10*time.Minute + time.Second)); len(p) != 1 {
		t.Errorf("problems past the 10m floor = %+v, want stale", p)
	}
}

func TestDisabledSource(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	r := newTestRegistry(&now)
	r.Track("catalog", time.Hour).Report(nil)
	r.Track("konflux", 0).Report(errors.New("create kubernetes client: no config"))

	p := r.Problems(now.Add(24 * time.Hour * 365))
	if len(p) != 2 || p[0].Source != "catalog" || p[1].Source != "konflux" {
		t.Fatalf("problems = %+v, want catalog (stale) then konflux (disabled)", p)
	}
	if p[1].Message != "create kubernetes client: no config" {
		t.Errorf("message = %q", p[1].Message)
	}
}

func TestNilSource(t *testing.T) {
	var s *Source
	s.Report(errors.New("ignored"))
}

func TestPass(t *testing.T) {
	var p Pass
	if p.Err() != nil {
		t.Fatalf("empty pass = %v, want nil", p.Err())
	}
	p.Add(nil)
	p.Add(errors.New("list releases: boom"))
	p.Add(errors.New("later"))
	p.Add(errors.New("later"))
	if got, want := p.Err().Error(), "list releases: boom (3 errors this pass)"; got != want {
		t.Errorf("Err = %q, want %q", got, want)
	}
}
