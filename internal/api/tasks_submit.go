package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

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

	created, err := s.store.Enqueue(r.Context(), t)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "enqueuing task failed")
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/tasks/%d", created.ID))
	writeJSON(w, http.StatusCreated, created)
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a bare error message. Task 059 replaces this with a
// unified error envelope shared by every handler.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
