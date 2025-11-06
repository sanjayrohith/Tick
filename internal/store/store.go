// Package store defines the backend-agnostic Store interface and its conformance suite.
//
// Two implementations exist: Postgres, which is the default and is correct for
// almost everyone, and a Redis Streams hybrid that caches imminent work for
// deployments past roughly 10M tasks/day. Both must pass the same conformance
// suite, because a backend that is subtly different is worse than one backend.
//
// # The clock rule
//
// No method on this interface accepts "now" from the caller. Every time-based
// decision (is this task due, has this heartbeat expired, when should the retry
// fire) is made by the database using its own clock. This is deliberate: a node
// 200ms ahead of its peers would otherwise fire jobs early, and with sub-minute
// schedules that produces real ordering bugs. There is exactly one clock in the
// system that matters, and it lives in Postgres.
package store

import (
	"context"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Store is the persistence and claiming interface every backend implements.
//
// Implementations must be safe for concurrent use by any number of goroutines
// and any number of processes. Concurrency correctness is not optional here: the
// entire point of the project is that two workers never execute one task.
type Store interface {
	Enqueuer
	Claimer
	Inspector

	// Close releases pooled connections. It is safe to call more than once.
	Close() error
}

// Enqueuer submits new work.
type Enqueuer interface {
	// Enqueue inserts one task and returns it with its assigned ID and any
	// database-applied defaults.
	//
	// When the task carries an idempotency key that already exists in the same
	// queue, Enqueue returns the existing task and ErrDuplicateIdempotencyKey.
	// Both are returned: the caller usually wants to treat this as success, and
	// needs the original task to report back to the client.
	Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error)

	// EnqueueBulk inserts many tasks in one transaction and returns them in
	// submission order. Tasks whose idempotency key already exists resolve to
	// the existing row rather than failing the batch, so one duplicate in a
	// thousand-task submission does not reject the other 999.
	EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error)
}

// Claimer is the worker-facing half of the interface: taking work, reporting
// progress, and reporting outcomes.
type Claimer interface {
	// Claim atomically transitions up to limit due tasks from pending to
	// running, stamps them with workerID, increments their attempt counts, and
	// returns them.
	//
	// Two concurrent callers must never receive the same task. In Postgres this
	// is one UPDATE over a FOR UPDATE SKIP LOCKED subquery: concurrent workers
	// skip each other's locked rows rather than queueing behind them, so adding
	// workers adds throughput instead of latency.
	//
	// Returns an empty slice, not an error, when nothing is due.
	Claim(ctx context.Context, queue, workerID string, limit int) ([]*domain.Task, error)

	// Heartbeat refreshes heartbeat_at for tasks still claimed by workerID and
	// returns how many rows were actually refreshed.
	//
	// A count lower than len(ids) means the worker lost at least one claim,
	// almost always because it stalled past the heartbeat TTL and the sweeper
	// reclaimed the row. The caller should stop executing those tasks: another
	// worker may already be running them.
	Heartbeat(ctx context.Context, ids []int64, workerID string) (refreshed int, err error)

	// Complete marks a task succeeded. It fails with ErrClaimLost if workerID no
	// longer holds the claim, which stops a resurrected zombie worker from
	// reporting success for a task that has since been reassigned.
	Complete(ctx context.Context, id int64, workerID string) error

	// Fail records an execution failure and applies the retry policy: back to
	// pending with a backed-off run_at while attempts remain, or to dead once
	// attempts reach max_attempts.
	//
	// The backoff interval is computed by the database, for the same reason
	// run_at comparisons are.
	Fail(ctx context.Context, id int64, workerID string, execErr error) error
}

// Inspector serves the read and administrative side of the API.
type Inspector interface {
	// Get returns one task by ID, or ErrNotFound.
	Get(ctx context.Context, id int64) (*domain.Task, error)

	// Cancel transitions a pending task to cancelled. It returns
	// ErrNotCancellable when the task is already running or terminal, because
	// once execution has begun the side effects are in flight.
	Cancel(ctx context.Context, id int64) error

	// QueueStats reports depth, age, and throughput for one queue.
	QueueStats(ctx context.Context, queue string) (*QueueStats, error)

	// RecoverOrphans reclaims tasks whose owning worker stopped heartbeating,
	// returning them to pending with a backed-off run_at, or to dead when their
	// attempts are exhausted. Returns how many rows were recovered.
	//
	// Backoff on recovery matters. If the worker died because the task OOMs the
	// process, an immediate retry just kills the next worker.
	RecoverOrphans(ctx context.Context) (recovered int, err error)
}

// QueueStats is a point-in-time view of one queue, backing the
// GET /queues/:name/stats endpoint and the queue depth metrics.
type QueueStats struct {
	// Queue is the queue these numbers describe.
	Queue string `json:"queue"`

	// Depth counts tasks by status. Every status in domain.AllStatuses is
	// present, including zeroes, so a consumer never has to distinguish "none"
	// from "not reported".
	Depth map[domain.Status]int64 `json:"depth"`

	// OldestPendingAgeSeconds is how long the oldest claimable task has been
	// waiting past its run_at. This is the number that actually tells you
	// whether the fleet is keeping up; queue depth alone does not.
	OldestPendingAgeSeconds float64 `json:"oldest_pending_age_seconds"`

	// Completed counts terminal transitions over trailing windows.
	CompletedLastMinute int64 `json:"completed_last_minute"`
	CompletedLastHour   int64 `json:"completed_last_hour"`
	CompletedLastDay    int64 `json:"completed_last_day"`
}

// NewQueueStats returns stats with every status pre-populated at zero.
func NewQueueStats(queue string) *QueueStats {
	depth := make(map[domain.Status]int64, len(domain.AllStatuses()))
	for _, s := range domain.AllStatuses() {
		depth[s] = 0
	}
	return &QueueStats{Queue: queue, Depth: depth}
}
