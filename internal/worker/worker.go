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

	// ShutdownGrace is how long Run waits for in-flight dispatches to finish,
	// once its context is cancelled, before force-releasing their claims.
	ShutdownGrace time.Duration
}

// errShutdownTimeout marks a task released because the worker's shutdown
// grace period elapsed before the task finished.
var errShutdownTimeout = errors.New("worker: shutdown grace period elapsed before task finished")

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

// Run claims and dispatches tasks from cfg.Queue until ctx is cancelled. On
// cancellation it stops claiming immediately, keeps heartbeating whatever is
// still in flight, and waits up to cfg.ShutdownGrace for those dispatches to
// finish before force-releasing any that have not, so a SIGTERM does not
// orphan work the sweeper would otherwise have to notice and recover later.
func (w *Worker) Run(ctx context.Context) {
	// Detached from ctx's cancellation but not its values, so a heartbeat or
	// a shutdown release can still run -- and still carry request-scoped log
	// attributes -- after ctx itself has already fired.
	bg := context.WithoutCancel(ctx)

	hbCtx, hbCancel := context.WithCancel(bg)
	defer hbCancel()
	go w.heartbeatLoop(hbCtx)

	for {
		// Block until at least one execution slot is free rather than busy
		// looping: a send on a buffered channel only succeeds when it is not
		// already full, and nothing here actually consumes the value sent.
		select {
		case w.sem <- struct{}{}:
			<-w.sem
		case <-ctx.Done():
			w.shutdown(bg)
			return
		}

		tasks, err := w.store.Claim(ctx, w.cfg.Queue, w.id, w.availableBatch())
		if err != nil {
			if ctx.Err() != nil {
				w.shutdown(bg)
				return
			}
			w.log.ErrorContext(ctx, "claim failed", "error", err)
			continue
		}

		for _, t := range tasks {
			w.sem <- struct{}{}
			w.wg.Add(1)
			// taskCtx is rooted in bg, not ctx: a dispatch in progress when
			// Run's context is cancelled keeps running through the grace
			// period instead of being cut off the instant shutdown begins.
			taskCtx, cancel := context.WithCancel(bg)
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

// shutdown waits up to cfg.ShutdownGrace for in-flight dispatches to finish,
// then cancels and releases whatever is still running.
func (w *Worker) shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(w.cfg.ShutdownGrace):
		w.releaseRemaining(ctx)
	}
}

// releaseRemaining cancels every still-in-flight task's context and fails it
// back to pending (or dead, once attempts are exhausted), so the sweeper does
// not have to wait out the full heartbeat TTL to notice this worker is gone.
func (w *Worker) releaseRemaining(ctx context.Context) {
	for _, id := range w.inFlight.ids() {
		if cancel, ok := w.inFlight.cancelFunc(id); ok {
			cancel()
		}
		if err := w.store.Fail(ctx, id, w.id, errShutdownTimeout); err != nil && !errors.Is(err, domain.ErrClaimLost) {
			w.log.ErrorContext(ctx, "releasing claim on shutdown failed", "task_id", id, "error", err)
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
