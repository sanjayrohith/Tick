package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// maxBulkTasks bounds one bulk submission. Past this a client should split
// the request rather than hold one transaction open over an arbitrarily long
// batch.
const maxBulkTasks = 1000

// bulkItemResult reports the outcome for one task in a bulk submission, so a
// partial idempotency hit is visible to the caller instead of hiding inside
// an otherwise-successful batch.
type bulkItemResult struct {
	Index  int          `json:"index"`
	Status string       `json:"status"` // "created", "duplicate", or "invalid"
	Task   *domain.Task `json:"task,omitempty"`
	Errors []fieldError `json:"errors,omitempty"`
}

func (s *Server) submitTasksBulk(w http.ResponseWriter, r *http.Request) {
	var subs []taskSubmission
	if err := json.NewDecoder(r.Body).Decode(&subs); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decoding request body: "+err.Error())
		return
	}
	if len(subs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "at least one task is required")
		return
	}
	if len(subs) > maxBulkTasks {
		writeJSONError(w, http.StatusBadRequest, "a bulk submission accepts at most 1000 tasks")
		return
	}

	// valid maps each accepted submission's position in tasks back to its
	// position in subs, so results can be assembled in original request order
	// even though only the valid subset is sent to the store.
	tasks := make([]*domain.Task, 0, len(subs))
	valid := make([]int, 0, len(subs))
	results := make([]bulkItemResult, len(subs))

	for i, sub := range subs {
		errs := validateSubmission(sub)
		if len(errs) > 0 {
			results[i] = bulkItemResult{Index: i, Status: "invalid", Errors: errs}
			continue
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
			t.RunAt, _ = time.Parse(time.RFC3339, *sub.RunAt)
		}
		if t.Queue == "" {
			t.Queue = "default"
		}
		tasks = append(tasks, t)
		valid = append(valid, i)
	}

	if len(tasks) > 0 {
		// requestStart is the boundary EnqueueBulk's returned rows are judged
		// against: a row created before this instant already existed, so
		// EnqueueBulk resolved that item to a duplicate rather than inserting
		// it. EnqueueBulk itself does not report this per item.
		requestStart := time.Now()
		enqueued, err := s.store.EnqueueBulk(r.Context(), tasks)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "enqueuing tasks failed")
			return
		}
		for j, t := range enqueued {
			i := valid[j]
			status := "created"
			if t.IdempotencyKey != nil && t.CreatedAt.Before(requestStart) {
				status = "duplicate"
			}
			results[i] = bulkItemResult{Index: i, Status: status, Task: t}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
