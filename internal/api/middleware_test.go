package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRecovererCatchesPanic(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	r := chi.NewRouter()
	r.Use(recoverer(log))
	r.Get("/panic", panicky.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error.Code != "internal" {
		t.Errorf("error.code = %q, want %q", body.Error.Code, "internal")
	}
}

func TestRequestLoggerEmitsOneLine(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler := requestLogger(log)(ok)
	req := httptest.NewRequest(http.MethodGet, "/brew", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	line := buf.String()
	if !strings.Contains(line, `"status":418`) {
		t.Errorf("log line missing status 418: %s", line)
	}
	if !strings.Contains(line, `"path":"/brew"`) {
		t.Errorf("log line missing path: %s", line)
	}
}

func TestMaxBodyBytesRejectsOversizedBody(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := maxBodyBytes(4)(echo)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("way too long"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSetRequestIDHeaderNoopWithoutID(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := setRequestIDHeader(ok)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "" {
		t.Errorf("X-Request-Id = %q, want empty when chi never set one", got)
	}
}
