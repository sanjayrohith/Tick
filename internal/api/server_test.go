package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSetsRequestIDHeaderOnMatchedRoute(t *testing.T) {
	s := New(nil, DefaultConfig())
	s.router.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header not set")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewReturnsNotFoundForUnregisteredRoute(t *testing.T) {
	s := New(nil, DefaultConfig())
	s.router.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for an unregistered route", rec.Code, http.StatusNotFound)
	}
}

func TestHTTPServerSetsTimeouts(t *testing.T) {
	cfg := DefaultConfig()
	s := New(nil, cfg)

	httpSrv := s.HTTPServer(":8080")

	if httpSrv.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", httpSrv.Addr, ":8080")
	}
	if httpSrv.ReadTimeout != cfg.ReadTimeout {
		t.Errorf("ReadTimeout = %s, want %s", httpSrv.ReadTimeout, cfg.ReadTimeout)
	}
	if httpSrv.WriteTimeout != cfg.WriteTimeout {
		t.Errorf("WriteTimeout = %s, want %s", httpSrv.WriteTimeout, cfg.WriteTimeout)
	}
	if httpSrv.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("IdleTimeout = %s, want %s", httpSrv.IdleTimeout, cfg.IdleTimeout)
	}
	if httpSrv.Handler == nil {
		t.Error("Handler is nil")
	}
}
