package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func decodeBulkResults(t *testing.T, rec *httptest.ResponseRecorder) []bulkItemResult {
	t.Helper()
	var body struct {
		Results []bulkItemResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v, body: %s", err, rec.Body.String())
	}
	return body.Results
}

func TestSubmitTasksBulkCreatesEveryValidTask(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	body := `[{"handler":"a"},{"handler":"b"},{"handler":"c"}]`
	req := httptest.NewRequest(http.MethodPost, "/tasks/bulk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	results := decodeBulkResults(t, rec)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for i, r := range results {
		if r.Status != "created" {
			t.Errorf("result %d: status = %q, want created", i, r.Status)
		}
		if r.Task == nil || r.Task.ID == 0 {
			t.Errorf("result %d: task not populated", i)
		}
	}
}

func TestSubmitTasksBulkReportsInvalidItemsWithoutFailingOthers(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	body := `[{"handler":"a"},{"handler":""},{"handler":"c"}]`
	req := httptest.NewRequest(http.MethodPost, "/tasks/bulk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	results := decodeBulkResults(t, rec)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Status != "created" || results[2].Status != "created" {
		t.Errorf("valid items were not created: %+v", results)
	}
	if results[1].Status != "invalid" || !hasField(results[1].Errors, "handler") {
		t.Errorf("invalid item = %+v, want status invalid with a handler error", results[1])
	}
}

func TestSubmitTasksBulkReportsDuplicates(t *testing.T) {
	existing := &domain.Task{
		ID:             99,
		Handler:        "a",
		IdempotencyKey: strPtr("key-1"),
		CreatedAt:      time.Now().Add(-time.Hour),
	}
	fs := &fakeStore{
		enqueueBulkFunc: func(_ context.Context, tasks []*domain.Task) ([]*domain.Task, error) {
			out := make([]*domain.Task, len(tasks))
			for i, task := range tasks {
				if task.IdempotencyKey != nil && *task.IdempotencyKey == "key-1" {
					out[i] = existing
					continue
				}
				fresh := *task
				fresh.ID = int64(i + 1)
				fresh.CreatedAt = time.Now()
				out[i] = &fresh
			}
			return out, nil
		},
	}
	s := New(fs, nil, DefaultConfig())

	body := `[{"handler":"a","idempotency_key":"key-1"},{"handler":"b"}]`
	req := httptest.NewRequest(http.MethodPost, "/tasks/bulk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	results := decodeBulkResults(t, rec)
	if results[0].Status != "duplicate" {
		t.Errorf("results[0].Status = %q, want duplicate", results[0].Status)
	}
	if results[1].Status != "created" {
		t.Errorf("results[1].Status = %q, want created", results[1].Status)
	}
}

func TestSubmitTasksBulkRejectsEmptyBatch(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks/bulk", strings.NewReader(`[]`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSubmitTasksBulkRejectsOverLimitBatch(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	items := make([]string, maxBulkTasks+1)
	for i := range items {
		items[i] = `{"handler":"h"}`
	}
	body := "[" + strings.Join(items, ",") + "]"

	req := httptest.NewRequest(http.MethodPost, "/tasks/bulk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
