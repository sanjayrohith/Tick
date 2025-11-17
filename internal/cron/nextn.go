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
		out = append(out, t.UTC())
	}
	return out
}
