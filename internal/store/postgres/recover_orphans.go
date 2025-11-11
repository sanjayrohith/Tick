package postgres

import (
	"context"
	"fmt"
)

// recoverOrphansSQL is the exact PRD statement: reclaim any running task whose
// heartbeat has gone silent for 90 seconds -- comfortably wider than the 10
// second heartbeat interval, so one missed beat cannot orphan a live task --
// applying the same capped exponential backoff as a reported failure so a
// task that kills its worker does not immediately kill the next one too.
const recoverOrphansSQL = `
UPDATE tasks
SET status = 'pending',
    run_at = now() + (interval '1 second' * least(300, power(2, attempts) * 5)),
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
