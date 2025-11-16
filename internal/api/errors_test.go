package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestWriteStoreErrorMapsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStoreError(rec, domain.ErrNotFound, "fallback")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Error.Code)
	}
}

func TestWriteStoreErrorMapsNotCancellable(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStoreError(rec, domain.ErrNotCancellable, "fallback")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestWriteStoreErrorNeverLeaksDriverText(t *testing.T) {
	driverErr := errors.New(`pq: syntax error at or near "SELECT * FROM secrets"`)
	rec := httptest.NewRecorder()
	writeStoreError(rec, driverErr, "operation failed")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error.Message != "operation failed" {
		t.Errorf("message = %q, want the generic fallback, not driver text", body.Error.Message)
	}
}
