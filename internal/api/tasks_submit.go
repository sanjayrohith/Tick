package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
	"github.com/sanjayrohith/tick/internal/store"
)

// idempotencyKeyHeader lets a client supply the idempotency key out of band,
// for callers that would rather not thread it through the body. It applies
// only if the body did not already set one -- the body takes precedence.
const idempotencyKeyHeader = "Idempotency-Key"

// Store is the persistence surface the API needs. It is a subset of
// store.Store: the API submits and inspects tasks, but never claims,
// heartbeats, or completes them -- that is the worker's job.
type Store interface {
	// Enqueue inserts one task and returns it with its assigned ID and any
	// database-applied defaults.
	Enqueue(ctx context.Context, t *domain.Task) (*domain.Task, error)

	// EnqueueBulk inserts many tasks in one transaction and returns them in
	// submission order.
	EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error)

	// Get returns one task by ID, or domain.ErrNotFound.
	Get(ctx context.Context, id int64) (*domain.Task, error)

	// Cancel transitions a pending task to cancelled. It returns
	// domain.ErrNotCancellable when the task is already running or terminal.
	Cancel(ctx context.Context, id int64) error

	// QueueStats reports depth, age, and throughput for one queue.
	QueueStats(ctx context.Context, queue string) (*store.QueueStats, error)

	// Ping checks that the store is reachable, backing the /readyz probe.
	Ping(ctx context.Context) error

	// CreateSchedule inserts one recurring schedule and returns it with its
	// assigned ID and any database-applied defaults.
	CreateSchedule(ctx context.Context, sc *domain.Schedule) (*domain.Schedule, error)

	// ListSchedules returns every schedule, enabled or not.
	ListSchedules(ctx context.Context) ([]*domain.Schedule, error)

	// GetSchedule returns one schedule by ID, or domain.ErrNotFound.
	GetSchedule(ctx context.Context, id int64) (*domain.Schedule, error)

	// SetScheduleEnabled enables or disables a schedule and returns it as
	// updated, or domain.ErrNotFound.
	SetScheduleEnabled(ctx context.Context, id int64, enabled bool) (*domain.Schedule, error)
}

// taskSubmission is the request body for POST /tasks. RunAt is a string, not
// a time.Time, so a malformed timestamp produces a field-level validation
// error instead of a generic JSON decode failure.
type taskSubmission struct {
	Queue          string          `json:"queue"`
	Handler        string          `json:"handler"`
	Payload        json.RawMessage `json:"payload"`
	Priority       int             `json:"priority"`
	RunAt          *string         `json:"run_at"`
	MaxAttempts    int             `json:"max_attempts"`
	IdempotencyKey *string         `json:"idempotency_key"`
}

// routes registers the API's endpoints on the server's router.
func (s *Server) routes() {
	s.router.Post("/tasks", s.submitTask)
	s.router.Post("/tasks/bulk", s.submitTasksBulk)
	s.router.Get("/tasks/{id}", s.getTask)
	s.router.Delete("/tasks/{id}", s.cancelTask)
	s.router.Get("/queues/{name}/stats", s.getQueueStats)
	s.router.Post("/schedules", s.createSchedule)
	s.router.Get("/schedules", s.listSchedules)
	s.router.Get("/schedules/{id}", s.getSchedule)
	s.router.Patch("/schedules/{id}", s.updateSchedule)
	s.router.Get("/healthz", s.healthz)
	s.router.Get("/readyz", s.readyz)
}

func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	var sub taskSubmission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("decoding request body: %v", err))
		return
	}

	if errs := validateSubmission(sub); len(errs) > 0 {
		writeValidationError(w, errs)
		return
	}

	t := &domain.Task{
		Queue:          sub.Queue,
		Handler:        sub.Handler,
		Payload:        sub.Payload,
		Priority:       sub.Priority,
		MaxAttempts:    sub.MaxAttempts,
		IdempotencyKey: sub.IdempotencyKey,
	}
	if sub.RunAt != nil {
		// Already confirmed parseable by validateSubmission.
		t.RunAt, _ = time.Parse(time.RFC3339, *sub.RunAt)
	}
	if t.Queue == "" {
		t.Queue = "default"
	}
	if t.IdempotencyKey == nil {
		if key := r.Header.Get(idempotencyKeyHeader); key != "" {
			t.IdempotencyKey = &key
		}
	}

	created, err := s.store.Enqueue(r.Context(), t)
	replay := errors.Is(err, domain.ErrDuplicateIdempotencyKey)
	if err != nil && !replay {
		writeError(w, http.StatusInternalServerError, "internal", "enqueuing task failed", nil)
		return
	}

	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	w.Header().Set("Location", fmt.Sprintf("/tasks/%d", created.ID))
	writeJSON(w, status, created)
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
