package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Store is the store surface a Worker needs: claiming, heartbeating,
// completing, and failing tasks, plus checking a task's current owner after
// a heartbeat suggests the claim was lost.
type Store interface {
	Claim(ctx context.Context, queue, workerID string, limit int) ([]*domain.Task, error)
	Heartbeat(ctx context.Context, ids []int64, workerID string) (int, error)
	Complete(ctx context.Context, id int64, workerID string) error
	Fail(ctx context.Context, id int64, workerID string, execErr error) error
	Get(ctx context.Context, id int64) (*domain.Task, error)
}

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

	// HeartbeatInterval is how often in-flight claims are refreshed in one
	// batched call.
	HeartbeatInterval time.Duration

	// TaskTimeout bounds how long a single handler invocation may run before
	// it is cancelled and reported as a retryable failure.
	TaskTimeout time.Duration
}

// Worker claims tasks from one queue and dispatches them to registered
// handlers, running up to cfg.Concurrency of them at once.
type Worker struct {
	store    Store
	registry *Registry
	cfg      Config
	id       string
	log      *slog.Logger

	sem      chan struct{}
	wg       sync.WaitGroup
	inFlight *inFlightSet
}

// New builds a Worker identified by id, claiming from s and dispatching to
// registry. A nil logger discards output.
func New(s Store, registry *Registry, id string, cfg Config, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Worker{
		store: s, registry: registry, cfg: cfg, id: id, log: log,
		sem:      make(chan struct{}, cfg.Concurrency),
		inFlight: newInFlightSet(),
	}
}

// Run claims and dispatches tasks from cfg.Queue until ctx is cancelled, then
// waits for in-flight dispatches to finish before returning.
func (w *Worker) Run(ctx context.Context) {
	go w.heartbeatLoop(ctx)

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
			taskCtx, cancel := context.WithCancel(ctx)
			w.inFlight.add(t.ID, cancel)
			go func(t *domain.Task, taskCtx context.Context, cancel context.CancelFunc) {
				defer w.wg.Done()
				defer func() { <-w.sem }()
				defer w.inFlight.remove(t.ID)
				defer cancel()
				w.dispatch(taskCtx, t)
			}(t, taskCtx, cancel)
		}
	}
}

// availableBatch caps a claim at the worker's free execution capacity, so it
// never claims more tasks than it can immediately start running.
func (w *Worker) availableBatch() int {
	return min(w.cfg.ClaimBatch, cap(w.sem)-len(w.sem))
}

// dispatch runs one task's handler under a per-task deadline and records the
// outcome.
func (w *Worker) dispatch(ctx context.Context, t *domain.Task) {
	h, err := w.registry.Lookup(t.Handler)
	if err != nil {
		w.fail(ctx, t, err)
		return
	}

	hCtx, cancel := context.WithTimeout(ctx, w.cfg.TaskTimeout)
	defer cancel()

	if err := safeHandle(hCtx, h, *t); err != nil {
		if errors.Is(hCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("handler timed out after %s: %w", w.cfg.TaskTimeout, err)
		}
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
