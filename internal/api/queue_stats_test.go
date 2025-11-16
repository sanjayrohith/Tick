package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/tick/internal/store"
)

func TestGetQueueStatsReturnsStats(t *testing.T) {
	want := store.NewQueueStats("emails")
	want.OldestPendingAgeSeconds = 12.5
	want.CompletedLastMinute = 3

	fs := &fakeStore{
		queueStatsFunc: func(_ context.Context, queue string) (*store.QueueStats, error) {
			if queue != "emails" {
				t.Fatalf("QueueStats called with queue=%q, want emails", queue)
			}
			return want, nil
		},
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/queues/emails/stats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got store.QueueStats
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Queue != "emails" || got.OldestPendingAgeSeconds != 12.5 || got.CompletedLastMinute != 3 {
		t.Errorf("got %+v, want queue=emails oldest=12.5 completed_last_minute=3", got)
	}
}

func TestGetQueueStatsStoreErrorReturns500(t *testing.T) {
	fs := &fakeStore{
		queueStatsFunc: func(context.Context, string) (*store.QueueStats, error) {
			return nil, errStoreDown
		},
	}
	s := New(fs, nil, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/queues/emails/stats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
