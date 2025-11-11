package postgres

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/tick/internal/domain"
)

// failSQL records the failure, releases the claim, and applies the retry
// policy in the same statement, guarded by claimed_by exactly like
// completeSQL.
//
// While attempts remain, the task goes back to pending with a capped
// exponential backoff plus full jitter: least(300, power(2, attempts) * 5)
// seconds matching the PRD, scaled by random() so tasks that failed at the
// same instant do not all retry at the same instant too. Once attempts are
// exhausted the task moves to dead instead, with every other field left
// exactly as it was: a dead task is evidence, and evidence that has been
// mutated or deleted on the way to the shelf is worthless.
//
// The interval is computed in SQL, not via internal/retry, so the database
// clock stays authoritative the same way run_at comparisons already are.
const failSQL = `
UPDATE tasks
SET status = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'dead' END,
    run_at = CASE WHEN attempts < max_attempts
                  THEN now() + (interval '1 second' * (random() * least(300, power(2, attempts) * 5)))
                  ELSE run_at END,
    claimed_by = NULL,
    heartbeat_at = NULL,
    last_error = $3
WHERE id = $1 AND claimed_by = $2`

// Fail records an execution failure, releases the claim, and applies the
// retry policy: back to pending with a backed-off run_at while attempts
// remain, or to dead once attempts reach max_attempts.
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
