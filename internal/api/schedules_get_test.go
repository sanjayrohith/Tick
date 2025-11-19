package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestListSchedulesReturnsEmptyArrayNotNull(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %q, want []", got)
	}
}

func TestListSchedulesReturnsEveryScheduleEnabledOrNot(t *testing.T) {
	fake := &fakeStore{
		listSchedulesFunc: func(context.Context) ([]*domain.Schedule, error) {
			return []*domain.Schedule{
				{ID: 1, Enabled: true},
				{ID: 2, Enabled: false},
			}, nil
		},
	}
	s := New(fake, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var got []domain.Schedule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
}

func TestGetScheduleNotFound(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/schedules/42", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetScheduleRejectsNonIntegerID(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/schedules/not-an-id", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateScheduleEnablesAndDisables(t *testing.T) {
	var lastEnabled bool
	fake := &fakeStore{
		setScheduleEnabledFunc: func(_ context.Context, id int64, enabled bool) (*domain.Schedule, error) {
			lastEnabled = enabled
			return &domain.Schedule{ID: id, Enabled: enabled}, nil
		},
	}
	s := New(fake, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPatch, "/schedules/1", strings.NewReader(`{"enabled":false}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if lastEnabled {
		t.Error("SetScheduleEnabled was called with enabled=true, want false")
	}

	var got domain.Schedule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Enabled {
		t.Error("response schedule shows Enabled=true, want false")
	}
}

func TestUpdateScheduleRequiresEnabledField(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPatch, "/schedules/1", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUpdateScheduleNotFound(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPatch, "/schedules/999", strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
