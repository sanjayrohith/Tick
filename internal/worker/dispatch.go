package worker

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/sanjayrohith/tick/internal/domain"
)

// safeHandle calls h.Handle, converting a panic into an error carrying the
// stack trace. One bad handler must never take down the worker process: it
// should cost that task an attempt, not every task the worker is running.
func safeHandle(ctx context.Context, h Handler, t domain.Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v\n%s", r, debug.Stack())
		}
	}()
	return h.Handle(ctx, t)
}
