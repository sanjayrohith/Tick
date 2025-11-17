package cron

import (
	"fmt"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// NextN parses expr, resolves tz as an IANA location, and returns the next n
// fire times strictly after "after". Fire times are computed in the zone's
// local wall-clock terms and returned as UTC instants, so callers never have
// to reason about offsets themselves.
func NextN(expr, tz string, after time.Time, n int) ([]time.Time, error) {
	sched, err := Parse(expr)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", domain.ErrInvalidTimezone, tz, err)
	}
	return sched.NextN(loc, after, n), nil
}

// NextN returns the next n fire times strictly after "after", computed in
// loc's wall-clock terms and returned as UTC instants.
//
// Spring forward is handled explicitly: a wall-clock time that does not
// exist on a given date, because the clock jumped past it, is skipped rather
// than shifted to the nearest valid instant. Skipping happens naturally,
// because the underlying schedule steps forward by real duration additions
// that a nonexistent local time can never land on; the round trip below is a
// defensive check against that assumption, not the mechanism itself.
func (s *Schedule) NextN(loc *time.Location, after time.Time, n int) []time.Time {
	if n <= 0 {
		return nil
	}
	out := make([]time.Time, 0, n)
	t := after.In(loc)
	for len(out) < n {
		next := s.spec.Next(t)
		if next.IsZero() {
			break
		}
		t = next

		if !roundTrips(t, loc) {
			continue
		}
		out = append(out, t.UTC())
	}
	return out
}

// roundTrips reports whether t's wall-clock fields, reconstructed fresh in
// loc, reproduce the exact same instant. A mismatch means the local time as
// computed does not correspond to a genuinely occurring wall clock moment in
// loc -- the signature of a nonexistent time swallowed by a spring-forward
// gap -- and the candidate must not be reported as a fire time.
func roundTrips(t time.Time, loc *time.Location) bool {
	reconstructed := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	return reconstructed.Equal(t)
}
