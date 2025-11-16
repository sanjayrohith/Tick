package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestCancelTaskSucceeds(t *testing.T) {
	fs := &fakeStore{
		cancelFunc: func(_ context.Context, id int64) error {
			if id != 7 {
				t.Fatalf("Cancel called with id=%d, want 7", id)
			}
			return nil
		},
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodDelete, "/tasks/7", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestCancelTaskReturns404WhenMissing(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodDelete, "/tasks/7", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCancelTaskReturns409WhenNotCancellable(t *testing.T) {
	fs := &fakeStore{
		cancelFunc: func(context.Context, int64) error {
			return domain.ErrNotCancellable
		},
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodDelete, "/tasks/7", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestCancelTaskRejectsNonNumericID(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodDelete, "/tasks/nope", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
