package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

// EnqueueBulk inserts many tasks in one transaction using a pgx.Batch, so the
// round trips scale with network latency once rather than once per task, and
// returns them in submission order.
//
// A task whose idempotency key already exists resolves to the existing row
// rather than failing the whole batch: one duplicate in a thousand-task
// submission must not reject the other 999.
func (s *Store) EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning bulk enqueue transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for _, t := range tasks {
		var runAt *time.Time
		if !t.RunAt.IsZero() {
			runAt = &t.RunAt
		}
		maxAttempts := t.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = 5
		}
		batch.Queue(enqueueSQL,
			t.Queue, t.Handler, t.Payload, t.Priority, runAt, maxAttempts,
			t.IdempotencyKey, t.ScheduleID)
	}

	// duplicates records the index of every task whose insert was skipped by
	// ON CONFLICT DO NOTHING, so they can be re-selected once the batch itself
	// has been fully consumed and closed. Interleaving a fresh query with an
	// in-flight batch is not safe over the same connection.
	inserted := make([]*domain.Task, len(tasks))
	var duplicates []int

	results := tx.SendBatch(ctx, batch)
	for i := range tasks {
		row := results.QueryRow()
		t, scanErr := scanTask(row)
		switch {
		case scanErr == nil:
			inserted[i] = t
		case errors.Is(scanErr, pgx.ErrNoRows) && tasks[i].IdempotencyKey != nil:
			duplicates = append(duplicates, i)
		default:
			_ = results.Close()
			return nil, fmt.Errorf("bulk enqueuing task %d: %w", i, scanErr)
		}
	}
	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("closing bulk enqueue batch: %w", err)
	}

	for _, i := range duplicates {
		existing, err := scanTask(tx.QueryRow(ctx, selectByIdempotencyKeySQL,
			tasks[i].Queue, *tasks[i].IdempotencyKey))
		if err != nil {
			return nil, fmt.Errorf("resolving duplicate idempotency key for task %d: %w", i, err)
		}
		inserted[i] = existing
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing bulk enqueue: %w", err)
	}
	return inserted, nil
}
