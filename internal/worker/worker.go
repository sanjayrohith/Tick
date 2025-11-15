package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/sanjayrohith/tick/internal/domain"
	"github.com/sanjayrohith/tick/internal/store"
)

// Config tunes a Worker's polling behaviour.
type Config struct {
	// Queue is the queue this worker claims from.
	Queue string

	// ClaimBatch is the maximum number of tasks claimed per round trip.
	ClaimBatch int

	// Concurrency bounds how many tasks this worker executes at once.
	// Claiming is throttled to the same limit, so the worker never holds more
	// claims than it has capacity to run.
	Concurrency int
}

// Worker claims tasks from one queue and dispatches them to registered
// handlers, running up to cfg.Concurrency of them at once.
type Worker struct {
	store    store.Claimer
	registry *Registry
	cfg      Config
	id       string
	log      *slog.Logger

	sem chan struct{}
	wg  sync.WaitGroup
}

// New builds a Worker identified by id, claiming from s and dispatching to
// registry. A nil logger discards output.
func New(s store.Claimer, registry *Registry, id string, cfg Config, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Worker{
		store: s, registry: registry, cfg: cfg, id: id, log: log,
		sem: make(chan struct{}, cfg.Concurrency),
	}
}

// Run claims and dispatches tasks from cfg.Queue until ctx is cancelled, then
// waits for in-flight dispatches to finish before returning.
func (w *Worker) Run(ctx context.Context) {
	for {
		// Block until at least one execution slot is free rather than busy
		// looping: a send on a buffered channel only succeeds when it is not
		// already full, and nothing here actually consumes the value sent.
		select {
		case w.sem <- struct{}{}:
			<-w.sem
		case <-ctx.Done():
			w.wg.Wait()
			return
		}

		tasks, err := w.store.Claim(ctx, w.cfg.Queue, w.id, w.availableBatch())
		if err != nil {
			if ctx.Err() != nil {
				w.wg.Wait()
				return
			}
			w.log.ErrorContext(ctx, "claim failed", "error", err)
			continue
		}

		for _, t := range tasks {
			w.sem <- struct{}{}
			w.wg.Add(1)
			go func(t *domain.Task) {
				defer w.wg.Done()
				defer func() { <-w.sem }()
				w.dispatch(ctx, t)
			}(t)
		}
	}
}

// availableBatch caps a claim at the worker's free execution capacity, so it
// never claims more tasks than it can immediately start running.
func (w *Worker) availableBatch() int {
	return min(w.cfg.ClaimBatch, cap(w.sem)-len(w.sem))
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
