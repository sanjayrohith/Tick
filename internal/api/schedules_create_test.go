package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateScheduleReturnsScheduleAndNextFireTimes(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	body := `{"cron":"30 2 * * *","timezone":"UTC","handler":"send_digest","payload":{}}`
	req := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got scheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.ID == 0 {
		t.Error("ID not assigned")
	}
	if got.Cron != "30 2 * * *" || got.Handler != "send_digest" {
		t.Errorf("got cron=%q handler=%q, want the submitted values", got.Cron, got.Handler)
	}
	if len(got.NextFireTimes) != nextFireTimesShown {
		t.Errorf("len(NextFireTimes) = %d, want %d", len(got.NextFireTimes), nextFireTimesShown)
	}
	for i := 1; i < len(got.NextFireTimes); i++ {
		if !got.NextFireTimes[i].After(got.NextFireTimes[i-1]) {
			t.Errorf("NextFireTimes not strictly increasing at index %d: %v", i, got.NextFireTimes)
		}
	}
}

func TestCreateScheduleRejectsMissingCron(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/schedules",
		strings.NewReader(`{"handler":"send_digest","payload":{}}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if len(body.Error.Fields) != 1 || body.Error.Fields[0].Field != "cron" {
		t.Errorf("fields = %+v, want exactly one error naming cron", body.Error.Fields)
	}
}

func TestCreateScheduleRejectsInvalidCronExpression(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/schedules",
		strings.NewReader(`{"cron":"99 2 * * *","handler":"send_digest","payload":{}}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if len(body.Error.Fields) != 1 || body.Error.Fields[0].Field != "cron" {
		t.Errorf("fields = %+v, want exactly one error naming cron", body.Error.Fields)
	}
}

func TestCreateScheduleRejectsUnloadableTimezone(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/schedules",
		strings.NewReader(`{"cron":"0 0 * * *","timezone":"Mars/Olympus_Mons","handler":"send_digest","payload":{}}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if len(body.Error.Fields) != 1 || body.Error.Fields[0].Field != "timezone" {
		t.Errorf("fields = %+v, want exactly one error naming timezone", body.Error.Fields)
	}
}

func TestCreateScheduleRejectsMissingHandler(t *testing.T) {
	s := New(&fakeStore{}, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodPost, "/schedules",
		strings.NewReader(`{"cron":"0 0 * * *","payload":{}}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
