package postgres

import (
	"context"
	"fmt"
)

const heartbeatSQL = `
UPDATE tasks
SET heartbeat_at = now()
WHERE id = ANY($1) AND claimed_by = $2 AND status = 'running'`

// Heartbeat refreshes heartbeat_at for tasks still claimed by workerID and
// returns how many rows were actually refreshed. A count lower than len(ids)
// means the worker lost at least one claim, most likely to the sweeper after
// stalling past the heartbeat TTL.
func (s *Store) Heartbeat(ctx context.Context, ids []int64, workerID string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	tag, err := s.pool.Exec(ctx, heartbeatSQL, ids, workerID)
	if err != nil {
		return 0, fmt.Errorf("sending heartbeat for worker %q: %w", workerID, err)
	}
	return int(tag.RowsAffected()), nil
}
