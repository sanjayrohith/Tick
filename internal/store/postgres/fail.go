package postgres

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/tick/internal/domain"
)

// failSQL records the failure and releases the claim, guarded by claimed_by
// exactly like completeSQL. It intentionally stops at status = 'failed': the
// decision of whether this goes back to pending with a backoff or on to dead
// belongs to the retry policy, not to this statement.
const failSQL = `
UPDATE tasks
SET status = 'failed',
    claimed_by = NULL,
    heartbeat_at = NULL,
    last_error = $3
WHERE id = $1 AND claimed_by = $2`

// Fail records an execution failure and releases the claim. The status
// transition beyond 'failed' (back to pending with backoff, or to dead once
// attempts are exhausted) is applied by the retry policy.
func (s *Store) Fail(ctx context.Context, id int64, workerID string, execErr error) error {
	msg := ""
	if execErr != nil {
		msg = domain.TruncateError(execErr.Error())
	}

	tag, err := s.pool.Exec(ctx, failSQL, id, workerID, msg)
	if err != nil {
		return fmt.Errorf("failing task %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrClaimLost
	}
	return nil
}
