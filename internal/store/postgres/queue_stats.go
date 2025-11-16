package postgres

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/tick/internal/domain"
	"github.com/sanjayrohith/tick/internal/store"
)

// queueStatsSQL computes every number QueueStats needs in one round trip.
// Depth is a conditional count per status rather than a GROUP BY, so the
// query always returns exactly one row -- including for a queue with no rows
// at all -- and the Go side never has to fill in statuses the result omitted.
//
// CompletedLast{Minute,Hour,Day} is measured against created_at rather than a
// true completion timestamp: tasks carries no column recording when a task
// finished, only when it was submitted. This undercounts long-running tasks
// that were submitted well before the window but completed inside it, and is
// the best available signal until a completed_at column exists.
const queueStatsSQL = `
SELECT
  count(*) FILTER (WHERE status = 'pending')   AS pending,
  count(*) FILTER (WHERE status = 'running')   AS running,
  count(*) FILTER (WHERE status = 'succeeded') AS succeeded,
  count(*) FILTER (WHERE status = 'failed')    AS failed,
  count(*) FILTER (WHERE status = 'dead')      AS dead,
  count(*) FILTER (WHERE status = 'cancelled') AS cancelled,
  EXTRACT(EPOCH FROM (now() - min(run_at) FILTER (WHERE status = 'pending' AND run_at <= now()))),
  count(*) FILTER (WHERE status = 'succeeded' AND created_at >= now() - interval '1 minute'),
  count(*) FILTER (WHERE status = 'succeeded' AND created_at >= now() - interval '1 hour'),
  count(*) FILTER (WHERE status = 'succeeded' AND created_at >= now() - interval '1 day')
FROM tasks
WHERE queue = $1`

// QueueStats reports depth, age, and throughput for one queue. Depth is
// always fully populated across every domain.Status, including zero counts,
// even for a queue that has never had a single task submitted to it.
func (s *Store) QueueStats(ctx context.Context, queue string) (*store.QueueStats, error) {
	stats := store.NewQueueStats(queue)

	var pending, running, succeeded, failed, dead, cancelled int64
	var oldestAge *float64
	err := s.pool.QueryRow(ctx, queueStatsSQL, queue).Scan(
		&pending, &running, &succeeded, &failed, &dead, &cancelled,
		&oldestAge,
		&stats.CompletedLastMinute,
		&stats.CompletedLastHour,
		&stats.CompletedLastDay,
	)
	if err != nil {
		return nil, fmt.Errorf("getting queue stats for %q: %w", queue, err)
	}

	stats.Depth[domain.StatusPending] = pending
	stats.Depth[domain.StatusRunning] = running
	stats.Depth[domain.StatusSucceeded] = succeeded
	stats.Depth[domain.StatusFailed] = failed
	stats.Depth[domain.StatusDead] = dead
	stats.Depth[domain.StatusCancelled] = cancelled
	if oldestAge != nil {
		stats.OldestPendingAgeSeconds = *oldestAge
	}
	return stats, nil
}
