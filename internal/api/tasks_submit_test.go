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
