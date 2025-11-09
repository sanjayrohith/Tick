package postgres

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/tick/internal/domain"
)

// completeSQL guards the transition with claimed_by = $2: if a resurrected
// zombie worker calls Complete after the sweeper has already reclaimed and
// possibly reassigned this task, the guard fails to match and the caller
// finds out via ErrClaimLost instead of silently marking someone else's
// in-flight work as succeeded.
const completeSQL = `
UPDATE tasks
SET status = 'succeeded',
    claimed_by = NULL,
    heartbeat_at = NULL
WHERE id = $1 AND claimed_by = $2`

// Complete marks a task succeeded. It fails with ErrClaimLost if workerID no
// longer holds the claim.
func (s *Store) Complete(ctx context.Context, id int64, workerID string) error {
	tag, err := s.pool.Exec(ctx, completeSQL, id, workerID)
	if err != nil {
		return fmt.Errorf("completing task %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrClaimLost
	}
	return nil
}
