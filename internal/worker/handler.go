package worker

import (
	"context"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Handler executes one task.
//
// Implementations must be idempotent. Delivery is at-least-once: a claim can
// be lost and the same task recovered by the sweeper after a handler has
// already produced a side effect, so Handle may run more than once for one
// task ID, and a handler that is not safe to repeat will eventually repeat.
type Handler interface {
	Handle(ctx context.Context, t domain.Task) error
}

// HandlerFunc adapts a plain function to the Handler interface, the same way
// http.HandlerFunc adapts a function to http.Handler.
type HandlerFunc func(ctx context.Context, t domain.Task) error

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, t domain.Task) error {
	return f(ctx, t)
}
