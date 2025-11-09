package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

// taskColumns lists every column of the tasks table in the order scanTask
// expects them, keeping the SELECT list and the scan in one place so they
// cannot drift apart.
const taskColumns = `id, queue, handler, payload, priority, status, run_at,
	attempts, max_attempts, claimed_by, heartbeat_at, idempotency_key,
	schedule_id, last_error, created_at`

// scanTask reads one row shaped like taskColumns into a domain.Task.
func scanTask(row pgx.Row) (*domain.Task, error) {
	var t domain.Task
	err := row.Scan(
		&t.ID, &t.Queue, &t.Handler, &t.Payload, &t.Priority, &t.Status, &t.RunAt,
		&t.Attempts, &t.MaxAttempts, &t.ClaimedBy, &t.HeartbeatAt, &t.IdempotencyKey,
		&t.ScheduleID, &t.LastError, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

const enqueueSQL = `
INSERT INTO tasks (queue, handler, payload, priority, run_at, max_attempts, idempotency_key, schedule_id)
VALUES ($1, $2, $3, $4, COALESCE($5, now()), $6, $7, $8)
RETURNING ` + taskColumns

// Enqueue inserts one task and returns it with its assigned ID and any
// database-applied defaults. A zero RunAt defaults to the database clock, so
// immediate work never depends on a client's clock.
func (s *Store) Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error) {
	// A nil pointer lets COALESCE fall through to now() in SQL; passing the
	// zero Go time.Time would instead insert year 1, which is not "now".
	var runAt *time.Time
	if !t.RunAt.IsZero() {
		runAt = &t.RunAt
	}

	maxAttempts := t.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}

	row := s.pool.QueryRow(ctx, enqueueSQL,
		t.Queue, t.Handler, t.Payload, t.Priority, runAt, maxAttempts,
		t.IdempotencyKey, t.ScheduleID)

	inserted, err := scanTask(row)
	if err != nil {
		return nil, fmt.Errorf("enqueuing task: %w", err)
	}
	return inserted, nil
}
