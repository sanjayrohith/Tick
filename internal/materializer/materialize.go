package materializer

import (
	"context"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Enqueuer is the store surface Materialize needs: bulk-inserting the tasks
// generated from planned fire times.
type Enqueuer interface {
	// EnqueueBulk inserts many tasks in one transaction, resolving a task
	// whose idempotency key already exists to the existing row rather than
	// failing the whole batch.
	EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error)
}

// Materialize inserts one task per fire time, each keyed with
// domain.IdempotencyKeyFor(s.ID, fireTime) via s.TaskFor.
//
// Any number of scheduler nodes may call Materialize for the same schedule
// and the same fire times concurrently: the unique index on (queue,
// idempotency_key) turns every insert after the first into a no-op, so the
// batch relies on the database to swallow duplicates rather than checking
// for them itself.
func Materialize(ctx context.Context, store Enqueuer, s *domain.Schedule, fireTimes []time.Time) ([]*domain.Task, error) {
	if len(fireTimes) == 0 {
		return nil, nil
	}

	tasks := make([]*domain.Task, len(fireTimes))
	for i, fireTime := range fireTimes {
		tasks[i] = s.TaskFor(fireTime)
	}

	return store.EnqueueBulk(ctx, tasks)
}
