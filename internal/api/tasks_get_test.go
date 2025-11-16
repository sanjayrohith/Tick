package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestGetTaskReturnsTask(t *testing.T) {
	want := &domain.Task{ID: 42, Queue: "emails", Handler: "send_welcome", Status: domain.StatusRunning, Attempts: 2}
	fs := &fakeStore{
		getFunc: func(_ context.Context, id int64) (*domain.Task, error) {
			if id != 42 {
				t.Fatalf("Get called with id=%d, want 42", id)
			}
			return want, nil
		},
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/tasks/42", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got domain.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.ID != 42 || got.Status != domain.StatusRunning || got.Attempts != 2 {
		t.Errorf("got %+v, want id=42 status=running attempts=2", got)
	}
}

func TestGetTaskReturns404WhenMissing(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/tasks/999", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetTaskRejectsNonNumericID(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/tasks/not-a-number", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
