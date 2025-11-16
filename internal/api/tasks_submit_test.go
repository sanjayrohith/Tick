package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

var errStoreDown = errors.New("store: connection refused")

func TestSubmitTaskCreatesAndReturnsTask(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	body := `{"queue":"emails","handler":"send_welcome","payload":{"to":"a@example.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got domain.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.ID == 0 {
		t.Error("ID not assigned")
	}
	if got.Queue != "emails" || got.Handler != "send_welcome" {
		t.Errorf("got queue=%q handler=%q, want emails/send_welcome", got.Queue, got.Handler)
	}

	wantLocation := fmt.Sprintf("/tasks/%d", got.ID)
	if loc := rec.Header().Get("Location"); loc != wantLocation {
		t.Errorf("Location = %q, want %q", loc, wantLocation)
	}
}

func TestSubmitTaskRejectsMalformedJSON(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSubmitTaskRejectsMissingHandlerWithFieldError(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"queue":"emails"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var body struct {
		Errors []fieldError `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !hasField(body.Errors, "handler") {
		t.Errorf("errors = %v, want a handler field error", body.Errors)
	}
}

func TestSubmitTaskRejectsMalformedRunAt(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	body := `{"handler":"h","run_at":"not-a-timestamp"}`
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSubmitTaskDefaultsQueue(t *testing.T) {
	fs := &fakeStore{}
	fs.enqueueFunc = func(_ context.Context, task *domain.Task) (*domain.Task, error) {
		out := *task
		out.ID = 1
		return &out, nil
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"handler":"h"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var got domain.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Queue != "default" {
		t.Errorf("Queue = %q, want default", got.Queue)
	}
}

func TestSubmitTaskAppliesIdempotencyKeyHeader(t *testing.T) {
	var gotKey *string
	fs := &fakeStore{}
	fs.enqueueFunc = func(_ context.Context, task *domain.Task) (*domain.Task, error) {
		gotKey = task.IdempotencyKey
		out := *task
		out.ID = 1
		return &out, nil
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"handler":"h"}`))
	req.Header.Set("Idempotency-Key", "client-key-1")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if gotKey == nil || *gotKey != "client-key-1" {
		t.Errorf("IdempotencyKey = %v, want client-key-1", gotKey)
	}
}

func TestSubmitTaskBodyIdempotencyKeyWinsOverHeader(t *testing.T) {
	var gotKey *string
	fs := &fakeStore{}
	fs.enqueueFunc = func(_ context.Context, task *domain.Task) (*domain.Task, error) {
		gotKey = task.IdempotencyKey
		out := *task
		out.ID = 1
		return &out, nil
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{"handler":"h","idempotency_key":"body-key"}`))
	req.Header.Set("Idempotency-Key", "header-key")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if gotKey == nil || *gotKey != "body-key" {
		t.Errorf("IdempotencyKey = %v, want body-key", gotKey)
	}
}

func TestSubmitTaskReplayReturns200(t *testing.T) {
	existing := &domain.Task{ID: 5, Handler: "h", IdempotencyKey: strPtr("dup-key")}
	fs := &fakeStore{}
	fs.enqueueFunc = func(context.Context, *domain.Task) (*domain.Task, error) {
		return existing, domain.ErrDuplicateIdempotencyKey
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{"handler":"h","idempotency_key":"dup-key"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got domain.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.ID != 5 {
		t.Errorf("ID = %d, want 5 (the original task)", got.ID)
	}
}

func TestSubmitTaskStoreErrorReturns500(t *testing.T) {
	fs := &fakeStore{}
	fs.enqueueFunc = func(context.Context, *domain.Task) (*domain.Task, error) {
		return nil, errStoreDown
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"handler":"h"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
