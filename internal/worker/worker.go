package worker

import (
	"context"
	"log/slog"

	"github.com/sanjayrohith/tick/internal/domain"
	"github.com/sanjayrohith/tick/internal/store"
)

// Config tunes a Worker's polling behaviour.
type Config struct {
	// Queue is the queue this worker claims from.
	Queue string

	// ClaimBatch is the maximum number of tasks claimed per round trip.
	ClaimBatch int
}

// Worker claims tasks from one queue and dispatches them to registered
// handlers.
type Worker struct {
	store    store.Claimer
	registry *Registry
	cfg      Config
	id       string
	log      *slog.Logger
}

// New builds a Worker identified by id, claiming from s and dispatching to
// registry. A nil logger discards output.
func New(s store.Claimer, registry *Registry, id string, cfg Config, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Worker{store: s, registry: registry, cfg: cfg, id: id, log: log}
}

// Run claims and dispatches tasks from cfg.Queue until ctx is cancelled, then
// returns.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tasks, err := w.store.Claim(ctx, w.cfg.Queue, w.id, w.cfg.ClaimBatch)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.log.ErrorContext(ctx, "claim failed", "error", err)
			continue
		}

		for _, t := range tasks {
			w.dispatch(ctx, t)
		}
	}
}

// dispatch runs one task's handler and records the outcome.
func (w *Worker) dispatch(ctx context.Context, t *domain.Task) {
	h, err := w.registry.Lookup(t.Handler)
	if err != nil {
		w.fail(ctx, t, err)
		return
	}

	if err := h.Handle(ctx, *t); err != nil {
		w.fail(ctx, t, err)
		return
	}

	if err := w.store.Complete(ctx, t.ID, w.id); err != nil {
		w.log.ErrorContext(ctx, "complete failed", "task_id", t.ID, "error", err)
	}
}

func (w *Worker) fail(ctx context.Context, t *domain.Task, execErr error) {
	if err := w.store.Fail(ctx, t.ID, w.id, execErr); err != nil {
		w.log.ErrorContext(ctx, "fail failed", "task_id", t.ID, "error", err)
	}
}
