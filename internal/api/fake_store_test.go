package api

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// fakeStore is an in-memory Store double for tests. Handlers are exercised
// against it directly rather than a real Postgres backend, so these tests run
// without Docker and stay fast; store.Store's own conformance suite is what
// proves the real backend behaves the same way.
type fakeStore struct {
	nextID int64

	enqueueFunc     func(ctx context.Context, t *domain.Task) (*domain.Task, error)
	enqueueBulkFunc func(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error)
	getFunc         func(ctx context.Context, id int64) (*domain.Task, error)
	cancelFunc      func(ctx context.Context, id int64) error
}

func (f *fakeStore) Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error) {
	if f.enqueueFunc != nil {
		return f.enqueueFunc(ctx, t)
	}
	out := *t
	out.ID = atomic.AddInt64(&f.nextID, 1)
	out.Status = domain.StatusPending
	out.CreatedAt = time.Now()
	return &out, nil
}

func (f *fakeStore) Get(ctx context.Context, id int64) (*domain.Task, error) {
	if f.getFunc != nil {
		return f.getFunc(ctx, id)
	}
	return nil, domain.ErrNotFound
}

func (f *fakeStore) Cancel(ctx context.Context, id int64) error {
	if f.cancelFunc != nil {
		return f.cancelFunc(ctx, id)
	}
	return domain.ErrNotFound
}

func (f *fakeStore) EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error) {
	if f.enqueueBulkFunc != nil {
		return f.enqueueBulkFunc(ctx, tasks)
	}
	out := make([]*domain.Task, len(tasks))
	for i, t := range tasks {
		enqueued, err := f.Enqueue(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = enqueued
	}
	return out, nil
}
