package postgres

import (
	"context"
	"fmt"
)

// recoverOrphansSQL reclaims any running task whose heartbeat has gone silent
// for 90 seconds -- comfortably wider than the 10 second heartbeat interval,
// so one missed beat cannot orphan a live task.
//
// A recovered task respects max_attempts exactly like a reported failure
// does: while attempts remain it goes back to pending with the same capped
// exponential backoff, so a task that kills its worker does not immediately
// kill the next one too. Once attempts are exhausted it goes straight to dead
// instead of recycling forever. Without this, a task whose handler reliably
// OOMs the process would consume a worker, die, get recovered, get reclaimed,
// and die again in an infinite loop -- one bad task quietly burning the whole
// fleet's capacity rather than being surfaced as the poison task it is.
const recoverOrphansSQL = `
UPDATE tasks
SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'dead' END,
    run_at = CASE WHEN attempts < max_attempts
                  THEN now() + (interval '1 second' * least(300, power(2, attempts) * 5))
                  ELSE run_at END,
    claimed_by = NULL,
    last_error = 'orphaned: heartbeat expired'
WHERE status = 'running' AND heartbeat_at < now() - interval '90 seconds'`

// RecoverOrphans reclaims tasks whose owning worker stopped heartbeating,
// returning them to pending with a backed-off run_at, and returns how many
// rows were recovered.
func (s *Store) RecoverOrphans(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, recoverOrphansSQL)
	if err != nil {
		return 0, fmt.Errorf("recovering orphaned tasks: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
