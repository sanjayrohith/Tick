// Package materializer expands recurring schedules into concrete tasks.
package materializer

import (
	"time"

	"github.com/sanjayrohith/tick/internal/cron"
	"github.com/sanjayrohith/tick/internal/domain"
)

// MaxPlannedFireTimes bounds how many fire times a single Plan call may
// return. A schedule such as "@every 1s" would otherwise compute an
// unbounded number of fire times across a multi-day horizon; capping the
// count turns a pathological schedule into a slow catch-up over several
// materializer passes instead of a table-flooding single one.
const MaxPlannedFireTimes = 10000

// Plan computes every fire time schedule s owes, from its last materialized
// point up to now+horizon, clamped to MaxPlannedFireTimes.
//
// A schedule with no prior materialization starts from now: it owes nothing
// from before it existed. now is passed in rather than read from the system
// clock, for the same reason every other time-based decision in Tick is
// -- so a caller can compute against the database's clock, not the local
// node's.
func Plan(s *domain.Schedule, now time.Time, horizon time.Duration) ([]time.Time, error) {
	loc, err := s.Location()
	if err != nil {
		return nil, err
	}

	sched, err := cron.Parse(s.Cron)
	if err != nil {
		return nil, err
	}

	after := now
	if s.LastMaterializedAt != nil {
		after = *s.LastMaterializedAt
	}

	until := now.Add(horizon)
	candidates := sched.NextN(loc, after, MaxPlannedFireTimes)

	planned := candidates[:0:0]
	for _, c := range candidates {
		if c.After(until) {
			break
		}
		planned = append(planned, c)
	}
	return planned, nil
}
