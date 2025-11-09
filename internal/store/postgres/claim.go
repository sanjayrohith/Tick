package postgres

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/tick/internal/domain"
)

// claimSQL is the mechanical heart of the whole project: the single statement
// that lets any number of workers claim from the same queue concurrently
// without ever handing two of them the same task.
//
// The subquery and FOR UPDATE SKIP LOCKED are both load-bearing:
//
//   - The subquery does the filtering, ordering, and row locking; the outer
//     UPDATE only touches the exact id set the subquery locked. Locking and
//     updating in one statement is what makes the whole thing atomic: there is
//     no gap between "decide which rows to take" and "take them" for another
//     transaction to land in.
//   - SKIP LOCKED is what turns concurrency into throughput instead of
//     latency. Without it, a second worker's SELECT ... FOR UPDATE would block
//     behind the first worker's still-open transaction, so adding workers
//     would only add queueing delay. With it, a worker whose candidate rows
//     are already locked simply skips them and locks the next-best ones
//     instead, so N workers claiming concurrently do roughly N times the work
//     rather than serializing on the query.
//
// Every comparison uses now(), the database clock, never a worker's clock:
// see the store package doc for why that single clock matters.
//
// The UPDATE is wrapped in a CTE with an outer SELECT ... ORDER BY. This is
// not about which rows get claimed: the subquery's ORDER BY plus LIMIT
// already pins that down before FOR UPDATE ever locks a row. It is about what
// order RETURNING hands them back in. Postgres makes no promise that an
// UPDATE ... WHERE id IN (SELECT ... ORDER BY ...) returns rows in that
// subquery's order; RETURNING order otherwise follows physical row order,
// which is not what a caller means by "highest priority first".
const claimSQL = `
WITH claimed AS (
  UPDATE tasks
  SET status = 'running',
      claimed_by = $1,
      heartbeat_at = now(),
      attempts = attempts + 1
  WHERE id IN (
    SELECT id FROM tasks
    WHERE queue = $2 AND status = 'pending' AND run_at <= now()
    ORDER BY priority DESC, run_at
    FOR UPDATE SKIP LOCKED
    LIMIT $3
  )
  RETURNING *
)
SELECT ` + taskColumns + `
FROM claimed
ORDER BY priority DESC, run_at`

// Claim atomically transitions up to limit due tasks from pending to running,
// stamps them with workerID, increments their attempt counts, and returns
// them. Returns an empty slice, not an error, when nothing is due.
func (s *Store) Claim(ctx context.Context, queue, workerID string, limit int) ([]*domain.Task, error) {
	rows, err := s.pool.Query(ctx, claimSQL, workerID, queue, limit)
	if err != nil {
		return nil, fmt.Errorf("claiming tasks: %w", err)
	}
	defer rows.Close()

	claimed := make([]*domain.Task, 0, limit)
	for rows.Next() {
		t, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning claimed task: %w", scanErr)
		}
		claimed = append(claimed, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating claimed tasks: %w", err)
	}
	return claimed, nil
}
