package api

import (
	"context"
	"sync/atomic"

	"github.com/sanjayrohith/tick/internal/domain"
)

// fakeStore is an in-memory Store double for tests. Handlers are exercised
// against it directly rather than a real Postgres backend, so these tests run
// without Docker and stay fast; store.Store's own conformance suite is what
// proves the real backend behaves the same way.
type fakeStore struct {
	nextID int64

	enqueueFunc func(ctx context.Context, t *domain.Task) (*domain.Task, error)
}

func (f *fakeStore) Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error) {
	if f.enqueueFunc != nil {
		return f.enqueueFunc(ctx, t)
	}
	out := *t
	out.ID = atomic.AddInt64(&f.nextID, 1)
	out.Status = domain.StatusPending
	return &out, nil
}
