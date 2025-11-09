package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

// EnqueueBulk inserts many tasks in one transaction using a pgx.Batch, so the
// round trips scale with network latency once rather than once per task, and
// returns them in submission order.
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

	results := tx.SendBatch(ctx, batch)
	inserted := make([]*domain.Task, 0, len(tasks))
	for range tasks {
		row := results.QueryRow()
		t, scanErr := scanTask(row)
		if scanErr != nil {
			_ = results.Close()
			return nil, fmt.Errorf("bulk enqueuing task: %w", scanErr)
		}
		inserted = append(inserted, t)
	}
	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("closing bulk enqueue batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing bulk enqueue: %w", err)
	}
	return inserted, nil
}
