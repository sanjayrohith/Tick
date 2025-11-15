package worker

import (
	"context"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Handler executes one task.
type Handler interface {
	Handle(ctx context.Context, t domain.Task) error
}
