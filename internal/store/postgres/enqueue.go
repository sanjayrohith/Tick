package postgres

import (
	"context"
	"errors"
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

// enqueueSQL uses ON CONFLICT DO NOTHING rather than an error, because a
// client retrying a submission after a dropped response must get back the
// original task, not a duplicate-key error it has to special-case.
const enqueueSQL = `
INSERT INTO tasks (queue, handler, payload, priority, run_at, max_attempts, idempotency_key, schedule_id)
VALUES ($1, $2, $3, $4, COALESCE($5, now()), $6, $7, $8)
ON CONFLICT (queue, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING ` + taskColumns

const selectByIdempotencyKeySQL = `
SELECT ` + taskColumns + `
FROM tasks
WHERE queue = $1 AND idempotency_key = $2`

// querier is the pgx surface Enqueue needs: satisfied by both *pgxpool.Pool
// and pgx.Tx, so the duplicate-resolution path works identically for a lone
// Enqueue and for one statement inside EnqueueBulk's transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Enqueue inserts one task and returns it with its assigned ID and any
// database-applied defaults. A zero RunAt defaults to the database clock, so
// immediate work never depends on a client's clock.
//
// When the task carries an idempotency key that already exists in the same
// queue, Enqueue returns the existing task alongside ErrDuplicateIdempotencyKey.
func (s *Store) Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error) {
	return enqueueOne(ctx, s.pool, t)
}

func enqueueOne(ctx context.Context, q querier, t *domain.Task) (*domain.Task, error) {
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

	row := q.QueryRow(ctx, enqueueSQL,
		t.Queue, t.Handler, t.Payload, t.Priority, runAt, maxAttempts,
		t.IdempotencyKey, t.ScheduleID)

	inserted, err := scanTask(row)
	if err == nil {
		return inserted, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) || t.IdempotencyKey == nil {
		return nil, fmt.Errorf("enqueuing task: %w", err)
	}

	// ON CONFLICT DO NOTHING skipped the insert: a task with this idempotency
	// key already exists. Re-select it so the caller gets the original task
	// rather than an error it cannot act on.
	existing, selErr := scanTask(q.QueryRow(ctx, selectByIdempotencyKeySQL, t.Queue, *t.IdempotencyKey))
	if selErr != nil {
		return nil, fmt.Errorf("resolving duplicate idempotency key: %w", selErr)
	}
	return existing, domain.ErrDuplicateIdempotencyKey
}
